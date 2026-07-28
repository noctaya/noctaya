/*
Copyright 2026 The Noctaya Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package gateway is the Noctaya data plane: an OpenAI-compatible reverse proxy that
// sits in front of one LLMService. It buffers requests while the backend is cold,
// applies bounded-queue backpressure, and exposes the pending-request count as the
// demand signal the scaler turns into a KEDA scale-from-zero decision.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Environment variables read by ConfigFromEnv (and set by the operator-rendered Deployment).
const (
	EnvBackendURL        = "NOCTAYA_BACKEND_URL"
	EnvMaxQueue          = "NOCTAYA_MAX_QUEUE"
	EnvActivationTimeout = "NOCTAYA_ACTIVATION_TIMEOUT"
	EnvListenAddr        = "NOCTAYA_LISTEN_ADDR"
	EnvScalerListenAddr  = "NOCTAYA_SCALER_LISTEN_ADDR"
	EnvAggregatorAddr    = "NOCTAYA_AGGREGATOR_LISTEN_ADDR"
	EnvDemandAggregator  = "NOCTAYA_DEMAND_AGGREGATOR_URL"
	EnvGatewayID         = "NOCTAYA_GATEWAY_ID"
	EnvColdStartMode     = "NOCTAYA_COLDSTART_MODE"
	EnvHeartbeatInterval = "NOCTAYA_HEARTBEAT_INTERVAL"

	DefaultListenAddr       = ":8080"
	DefaultScalerListenAddr = ":9090"
	DefaultAggregatorAddr   = ":9091"
	QueuePath               = "/noctaya/queue"
	MetricsPath             = "/metrics"

	// ColdStartKeepalive holds a streaming request open with SSE heartbeats during a
	// cold start; ColdStartReject returns 503 + Retry-After immediately for the client
	// to retry once warm.
	ColdStartKeepalive = "keepalive"
	ColdStartReject    = "reject"

	// maxPeekBody bounds how much request body we buffer to detect a streaming request.
	maxPeekBody = 8 << 20
)

type Config struct {
	BackendURL        string
	MaxQueue          int
	ActivationTimeout time.Duration
	RetryInterval     time.Duration
	ColdStartMode     string
	HeartbeatInterval time.Duration
	// ActivationGracePeriod keeps demand raised after the backend first becomes ready,
	// giving reject-mode clients time to retry without an immediate scale-down.
	ActivationGracePeriod time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		BackendURL:    os.Getenv(EnvBackendURL),
		ColdStartMode: os.Getenv(EnvColdStartMode),
	}
	if v, err := strconv.Atoi(os.Getenv(EnvMaxQueue)); err == nil {
		cfg.MaxQueue = v
	}
	if d, err := time.ParseDuration(os.Getenv(EnvActivationTimeout)); err == nil {
		cfg.ActivationTimeout = d
	}
	if d, err := time.ParseDuration(os.Getenv(EnvHeartbeatInterval)); err == nil {
		cfg.HeartbeatInterval = d
	}
	return cfg
}

type metrics struct {
	registry         *prometheus.Registry
	pending          prometheus.Gauge
	demand           prometheus.Gauge
	requests         *prometheus.CounterVec
	rejections       *prometheus.CounterVec
	coldStart        prometheus.Histogram
	scalerStreams    prometheus.Gauge
	activationEvents prometheus.Counter
	demandReports    *prometheus.CounterVec
}

func newMetrics() *metrics {
	m := &metrics{
		registry: prometheus.NewRegistry(),
		pending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "noctaya_gateway_pending", Help: "Requests admitted and waiting or in flight."}),
		demand: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "noctaya_gateway_demand", Help: "Local effective queue demand, including an activation lease floor."}),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "noctaya_gateway_requests_total", Help: "Responses by HTTP status code."}, []string{"code"}),
		rejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "noctaya_gateway_rejections_total", Help: "Rejected requests by reason."}, []string{"reason"}),
		coldStart: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "noctaya_gateway_activation_wait_seconds", Help: "Time spent holding a request until the backend was ready.",
			Buckets: []float64{0.01, 0.1, 1, 5, 15, 30, 60, 120, 300}}),
		scalerStreams: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "noctaya_gateway_scaler_streams", Help: "Connected KEDA External Push activation streams."}),
		activationEvents: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "noctaya_gateway_activation_events_total", Help: "Inactive-to-active effective demand transitions."}),
		demandReports: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "noctaya_gateway_demand_reports_total", Help: "Demand reports sent to the aggregate scaler by result."}, []string{"result"}),
	}
	m.registry.MustRegister(
		m.pending,
		m.demand,
		m.requests,
		m.rejections,
		m.coldStart,
		m.scalerStreams,
		m.activationEvents,
		m.demandReports,
	)
	return m
}

type Gateway struct {
	cfg     Config
	backend *url.URL
	proxy   *httputil.ReverseProxy
	sem     chan struct{}
	pending atomic.Int64
	m       *metrics
	probe   *http.Client
	now     func() time.Time
	ctx     context.Context
	cancel  context.CancelFunc

	leaseMu         sync.Mutex
	leaseActive     bool
	leaseDeadline   time.Time
	readyGraceUntil time.Time
	leaseWatcher    bool

	notifyMu sync.Mutex
	demands  demandSubscriptions
}

type activationWaitResult int

const (
	activationReady activationWaitResult = iota
	activationTimedOut
	activationClientClosed
)

func New(cfg Config) (*Gateway, error) {
	u, err := url.Parse(cfg.BackendURL)
	if err != nil {
		return nil, err
	}
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = 100
	}
	if cfg.ActivationTimeout <= 0 {
		cfg.ActivationTimeout = 5 * time.Minute
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 500 * time.Millisecond
	}
	if cfg.ColdStartMode != ColdStartReject {
		cfg.ColdStartMode = ColdStartKeepalive
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 10 * time.Second
	}
	if cfg.ActivationGracePeriod <= 0 {
		cfg.ActivationGracePeriod = 15 * time.Second
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.FlushInterval = -1 // flush every write so tokens stream as they arrive
	ctx, cancel := context.WithCancel(context.Background())
	return &Gateway{
		cfg:     cfg,
		backend: u,
		proxy:   proxy,
		sem:     make(chan struct{}, cfg.MaxQueue),
		m:       newMetrics(),
		probe:   &http.Client{Timeout: 2 * time.Second},
		now:     time.Now,
		ctx:     ctx,
		cancel:  cancel,
		demands: newDemandSubscriptions(),
	}, nil
}

func (g *Gateway) Pending() int64 { return g.pending.Load() }

func (g *Gateway) Demand() int64 {
	pending := g.pending.Load()
	if pending > 0 {
		return pending
	}
	now := g.now()
	g.leaseMu.Lock()
	active := (g.leaseActive && now.Before(g.leaseDeadline)) || now.Before(g.readyGraceUntil)
	g.leaseMu.Unlock()
	if active {
		return 1
	}
	return 0
}

func (g *Gateway) Close() { g.cancel() }

func (g *Gateway) scalerStreamConnected() { g.m.scalerStreams.Inc() }

func (g *Gateway) scalerStreamDisconnected() { g.m.scalerStreams.Dec() }

func (g *Gateway) recordDemandReport(result string) {
	g.m.demandReports.WithLabelValues(result).Inc()
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc(QueuePath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"pending": g.Demand()})
	})
	mux.Handle(MetricsPath, promhttp.HandlerFor(g.m.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/", g.serve)
	return mux
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	// Bounded-queue backpressure: reject rather than buffer to OOM.
	select {
	case g.sem <- struct{}{}:
		defer func() { <-g.sem }()
	default:
		g.m.rejections.WithLabelValues("queue_full").Inc()
		g.m.requests.WithLabelValues("429").Inc()
		w.Header().Set("Retry-After", "5")
		http.Error(w, "gateway queue full", http.StatusTooManyRequests)
		return
	}

	// Demand signal for the scaler, raised for the whole hold-and-serve window.
	g.changePending(1)
	defer g.changePending(-1)

	waitStart := g.now()
	committed := false
	if !g.backendReady(r.Context()) {
		// Cold start: keep the demand visible to the scaler even if we return fast.
		g.beginActivation()
		switch {
		case g.cfg.ColdStartMode == ColdStartReject:
			g.reject(w, "cold_start")
			return
		case wantsStream(r):
			// keepalive: hold the streaming connection open with SSE heartbeats so the
			// client and intermediate proxies don't time out during the minutes-long load.
			result, streamCommitted := g.holdWithHeartbeat(w, r)
			if result != activationReady {
				if streamCommitted {
					g.m.requests.WithLabelValues("200").Inc()
				}
				if result == activationClientClosed {
					return
				}
				if !streamCommitted {
					g.reject(w, "activation_timeout")
					return
				}
				g.m.rejections.WithLabelValues("activation_timeout").Inc()
				g.writeStreamError(w)
				return
			}
			committed = streamCommitted
		default:
			// Non-streaming client: hold silently; heartbeats would corrupt a JSON body.
			result := g.waitForBackend(r.Context())
			if result == activationClientClosed {
				return
			}
			if result == activationTimedOut {
				g.reject(w, "activation_timeout")
				return
			}
		}
	}
	g.m.coldStart.Observe(g.now().Sub(waitStart).Seconds())

	if committed {
		// Headers were already sent for the heartbeat stream; suppress the proxy's.
		g.proxy.ServeHTTP(&committedWriter{ResponseWriter: w}, r)
		g.m.requests.WithLabelValues("200").Inc()
		return
	}
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	g.proxy.ServeHTTP(rec, r)
	g.m.requests.WithLabelValues(strconv.Itoa(rec.status)).Inc()
}

func (g *Gateway) reject(w http.ResponseWriter, reason string) {
	g.m.rejections.WithLabelValues(reason).Inc()
	g.m.requests.WithLabelValues("503").Inc()
	w.Header().Set("Retry-After", "10")
	http.Error(w, "backend not ready", http.StatusServiceUnavailable)
}

func (g *Gateway) changePending(delta int64) {
	g.m.pending.Set(float64(g.pending.Add(delta)))
	g.notifyDemandChange()
}

// beginActivation holds a demand lease independently of the client connection. This
// is essential for reject mode: the request can return 503 while KEDA is still
// starting the backend.
func (g *Gateway) beginActivation() {
	now := g.now()
	g.leaseMu.Lock()
	g.leaseActive = true
	g.leaseDeadline = now.Add(g.cfg.ActivationTimeout)
	startWatcher := !g.leaseWatcher
	if startWatcher {
		g.leaseWatcher = true
	}
	g.leaseMu.Unlock()
	g.notifyDemandChange()
	if startWatcher {
		go g.watchActivation()
	}
}

func (g *Gateway) watchActivation() {
	for {
		select {
		case <-g.ctx.Done():
			return
		default:
		}

		g.leaseMu.Lock()
		deadline := g.leaseDeadline
		g.leaseMu.Unlock()
		if !g.now().Before(deadline) && g.finishActivation(false) {
			return
		}
		probeCtx, cancel := context.WithDeadline(g.ctx, deadline)
		ready := g.backendReady(probeCtx)
		cancel()
		if ready {
			g.finishActivation(true)
			return
		}
		select {
		case <-g.ctx.Done():
			return
		case <-time.After(g.cfg.RetryInterval):
		}
	}
}

// finishActivation returns false when a newer request renewed a lease that the
// watcher had considered expired.
func (g *Gateway) finishActivation(ready bool) bool {
	now := g.now()
	g.leaseMu.Lock()
	if !ready && now.Before(g.leaseDeadline) {
		g.leaseMu.Unlock()
		return false
	}
	g.leaseActive = false
	g.leaseWatcher = false
	if ready {
		g.readyGraceUntil = now.Add(g.cfg.ActivationGracePeriod)
	}
	graceDeadline := g.readyGraceUntil
	g.leaseMu.Unlock()
	g.notifyDemandChange()
	if ready {
		go g.notifyAfterGrace(graceDeadline)
	}
	return true
}

func (g *Gateway) notifyAfterGrace(deadline time.Time) {
	delay := max(time.Duration(0), time.Until(deadline))
	select {
	case <-g.ctx.Done():
	case <-time.After(delay):
		g.notifyDemandChange()
	}
}

func (g *Gateway) notifyDemandChange() {
	g.notifyMu.Lock()
	defer g.notifyMu.Unlock()

	demand := g.Demand()
	g.m.demand.Set(float64(demand))
	if g.demands.publish(demand) {
		g.m.activationEvents.Inc()
	}
}

func (g *Gateway) subscribeDemand() (<-chan int64, func()) {
	g.notifyMu.Lock()
	ch, unsubscribe := g.demands.subscribe(g.Demand())
	g.notifyMu.Unlock()
	return ch, unsubscribe
}

// holdWithHeartbeat commits a 200 SSE response and emits keepalive comments until the
// backend is ready, the request is canceled, or the activation timeout elapses. The
// committed result keeps response-code metrics aligned with what the client received.
func (g *Gateway) holdWithHeartbeat(w http.ResponseWriter, r *http.Request) (activationWaitResult, bool) {
	if r.Context().Err() != nil {
		return activationClientClosed, false
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return g.waitForBackend(r.Context()), false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	deadline := g.now().Add(g.cfg.ActivationTimeout)
	lastBeat := g.now()
	for {
		if g.backendReady(ctx) {
			return activationReady, true
		}
		if g.now().After(deadline) {
			return activationTimedOut, true
		}
		if g.now().Sub(lastBeat) >= g.cfg.HeartbeatInterval {
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return activationClientClosed, true
			}
			flusher.Flush()
			lastBeat = g.now()
		}
		select {
		case <-ctx.Done():
			return activationClientClosed, true
		case <-time.After(g.cfg.RetryInterval):
		}
	}
}

func (g *Gateway) writeStreamError(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "event: error\ndata: backend activation timeout\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// wantsStream reports whether the client asked for a streaming response (so heartbeats
// are safe). It buffers and restores the body, leaving the proxied request intact.
func wantsStream(r *http.Request) bool {
	if r.Body == nil || r.Method == http.MethodGet {
		return false
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxPeekBody+1))
	if err != nil || len(buf) > maxPeekBody {
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), r.Body))
		return false
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(buf))
	var probe struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(buf, &probe)
	return probe.Stream
}

// committedWriter drops the proxied response's header write because the heartbeat path
// already sent 200 + SSE headers; the backend's body streams through unchanged.
type committedWriter struct {
	http.ResponseWriter
}

func (c *committedWriter) WriteHeader(int) {}

func (c *committedWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// waitForBackend blocks until the backend is ready, the request is canceled, or the
// activation timeout elapses (cold-start hold).
func (g *Gateway) waitForBackend(ctx context.Context) activationWaitResult {
	deadline := g.now().Add(g.cfg.ActivationTimeout)
	for {
		if ctx.Err() != nil {
			return activationClientClosed
		}
		if g.backendReady(ctx) {
			return activationReady
		}
		if g.now().After(deadline) {
			return activationTimedOut
		}
		select {
		case <-ctx.Done():
			return activationClientClosed
		case <-time.After(g.cfg.RetryInterval):
		}
	}
}

func (g *Gateway) backendReady(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.backend.String()+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := g.probe.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// statusRecorder captures the proxied response status for metrics while passing
// through streaming (SSE) flushes.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
