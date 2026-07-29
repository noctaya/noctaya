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

package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const maxPeekBody = 8 << 20

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

func (g *Gateway) serve(w http.ResponseWriter, request *http.Request) {
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

	g.changePending(1)
	defer g.changePending(-1)

	waitStart := g.now()
	committed := false
	waitedForActivation := false
	if !g.backendReady(request.Context()) {
		waitedForActivation = true
		g.beginActivation()
		switch {
		case g.cfg.ColdStartMode == ColdStartReject:
			g.reject(w, "cold_start")
			return
		case wantsStream(request):
			result, streamCommitted := g.holdWithHeartbeat(w, request)
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
			result := g.waitForBackend(request.Context())
			if result == activationClientClosed {
				return
			}
			if result == activationTimedOut {
				g.reject(w, "activation_timeout")
				return
			}
		}
	}
	if waitedForActivation {
		g.m.coldStart.Observe(g.now().Sub(waitStart).Seconds())
	}

	if committed {
		g.proxy.ServeHTTP(&committedWriter{ResponseWriter: w}, request)
		g.m.requests.WithLabelValues("200").Inc()
		return
	}
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	g.proxy.ServeHTTP(recorder, request)
	g.m.requests.WithLabelValues(strconv.Itoa(recorder.status)).Inc()
}

func (g *Gateway) reject(w http.ResponseWriter, reason string) {
	g.m.rejections.WithLabelValues(reason).Inc()
	g.m.requests.WithLabelValues("503").Inc()
	w.Header().Set("Retry-After", "10")
	http.Error(w, "backend not ready", http.StatusServiceUnavailable)
}

func wantsStream(request *http.Request) bool {
	if request.Body == nil || request.Method == http.MethodGet {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxPeekBody+1))
	if err != nil || len(body) > maxPeekBody {
		request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), request.Body))
		return false
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	var probe struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &probe)
	return probe.Stream
}

// committedWriter preserves an SSE response already committed during activation.
type committedWriter struct {
	http.ResponseWriter
}

func (c *committedWriter) WriteHeader(int) {}

func (c *committedWriter) Flush() {
	if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
