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

package scaler

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/noctaya/noctaya/internal/gateway/demand"
)

const (
	DemandReportPath = "/v1/demand"

	defaultMemberTTL     = 10 * time.Second
	defaultSweepInterval = time.Second
	defaultMaxMembers    = 1024
	defaultMaxDemand     = int64(100)
	defaultMaxConcurrent = 64
	maxDemandReportBody  = 64 << 10
	maxMemberIDLength    = 512
	demandReportAccepted = "accepted"
	demandReportStale    = "stale"
	demandReportCapacity = "capacity"
)

type DemandAggregatorConfig struct {
	MemberTTL            time.Duration
	SweepInterval        time.Duration
	AuthTokenFile        string
	MaxMembers           int
	MaxDemand            int64
	MaxConcurrentReports int
	Now                  func() time.Time
}

type demandReport struct {
	MemberID string `json:"memberID"`
	Sequence uint64 `json:"sequence"`
	Demand   int64  `json:"demand"`
}

type demandMember struct {
	sequence  uint64
	demand    int64
	expiresAt time.Time
}

type aggregatorMetrics struct {
	registry      *prometheus.Registry
	demand        prometheus.Gauge
	members       prometheus.Gauge
	reports       *prometheus.CounterVec
	expired       prometheus.Counter
	scalerStreams prometheus.Gauge
}

func newAggregatorMetrics() *aggregatorMetrics {
	metrics := &aggregatorMetrics{
		registry: prometheus.NewRegistry(),
		demand: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "noctaya_scaler_demand", Help: "Aggregate demand reported to KEDA."}),
		members: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "noctaya_scaler_gateway_members", Help: "Gateway members with unexpired demand reports."}),
		reports: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "noctaya_scaler_demand_reports_total", Help: "Gateway demand reports by result."}, []string{"result"}),
		expired: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "noctaya_scaler_expired_members_total", Help: "Gateway members removed after their reports expired."}),
		scalerStreams: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "noctaya_gateway_scaler_streams", Help: "Connected KEDA External Push activation streams."}),
	}
	metrics.registry.MustRegister(
		metrics.demand,
		metrics.members,
		metrics.reports,
		metrics.expired,
		metrics.scalerStreams,
	)
	return metrics
}

type DemandAggregator struct {
	memberTTL     time.Duration
	sweepInterval time.Duration
	now           func() time.Time
	metrics       *aggregatorMetrics
	authTokenFile string
	maxMembers    int
	maxDemand     int64
	reportSlots   chan struct{}

	mu      sync.Mutex
	members map[string]demandMember
	total   int64
	demands demand.Subscriptions
}

func NewDemandAggregator(cfg DemandAggregatorConfig) *DemandAggregator {
	if cfg.MemberTTL <= 0 {
		cfg.MemberTTL = defaultMemberTTL
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = defaultSweepInterval
	}
	if cfg.MaxMembers <= 0 {
		cfg.MaxMembers = defaultMaxMembers
	}
	if cfg.MaxDemand <= 0 {
		cfg.MaxDemand = defaultMaxDemand
	}
	if cfg.MaxConcurrentReports <= 0 {
		cfg.MaxConcurrentReports = defaultMaxConcurrent
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &DemandAggregator{
		memberTTL:     cfg.MemberTTL,
		sweepInterval: cfg.SweepInterval,
		now:           cfg.Now,
		metrics:       newAggregatorMetrics(),
		authTokenFile: cfg.AuthTokenFile,
		maxMembers:    cfg.MaxMembers,
		maxDemand:     cfg.MaxDemand,
		reportSlots:   make(chan struct{}, cfg.MaxConcurrentReports),
		members:       make(map[string]demandMember),
		demands:       demand.NewSubscriptions(),
	}
}

func (a *DemandAggregator) Run(ctx context.Context) {
	ticker := time.NewTicker(a.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.expire(a.now())
		}
	}
}

func (a *DemandAggregator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := demand.ReadAuthToken(a.authTokenFile); err != nil {
			http.Error(w, "demand authentication unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(MetricsPath, promhttp.HandlerFor(a.metrics.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc(DemandReportPath, a.handleDemandReport)
	return mux
}

func (a *DemandAggregator) Demand() int64 {
	a.expire(a.now())
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.total
}

func (a *DemandAggregator) SubscribeDemand() (<-chan int64, func()) {
	a.expire(a.now())
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.demands.Subscribe(a.total)
}

func (a *DemandAggregator) ScalerStreamConnected() { a.metrics.scalerStreams.Inc() }

func (a *DemandAggregator) ScalerStreamDisconnected() { a.metrics.scalerStreams.Dec() }

func (a *DemandAggregator) handleDemandReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	select {
	case a.reportSlots <- struct{}{}:
		defer func() { <-a.reportSlots }()
	default:
		http.Error(w, "too many demand reports", http.StatusTooManyRequests)
		return
	}
	token, err := demand.ReadAuthToken(a.authTokenFile)
	if err != nil {
		http.Error(w, "demand authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	if !demand.Authorized(r.Header.Get("Authorization"), token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDemandReportBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var report demandReport
	if err := decoder.Decode(&report); err != nil {
		http.Error(w, "invalid demand report", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "invalid demand report", http.StatusBadRequest)
		return
	}
	if report.MemberID == "" || len(report.MemberID) > maxMemberIDLength ||
		report.Sequence == 0 || report.Demand < 0 || report.Demand > a.maxDemand {
		http.Error(w, "invalid demand report", http.StatusBadRequest)
		return
	}
	result := a.update(report, a.now())
	a.metrics.reports.WithLabelValues(result).Inc()
	if result == demandReportCapacity {
		http.Error(w, "gateway member capacity reached", http.StatusTooManyRequests)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *DemandAggregator) update(report demandReport, now time.Time) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expireLocked(now)
	if existing, found := a.members[report.MemberID]; found {
		if report.Sequence <= existing.sequence {
			a.publishLocked()
			return demandReportStale
		}
	} else if len(a.members) >= a.maxMembers {
		a.publishLocked()
		return demandReportCapacity
	}
	a.members[report.MemberID] = demandMember{
		sequence:  report.Sequence,
		demand:    report.Demand,
		expiresAt: now.Add(a.memberTTL),
	}
	a.recalculateLocked()
	a.publishLocked()
	return demandReportAccepted
}

func (a *DemandAggregator) expire(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.expireLocked(now) > 0 {
		a.publishLocked()
	}
}

func (a *DemandAggregator) expireLocked(now time.Time) int {
	expired := 0
	for id, member := range a.members {
		if !now.Before(member.expiresAt) {
			delete(a.members, id)
			expired++
		}
	}
	if expired > 0 {
		a.metrics.expired.Add(float64(expired))
		a.recalculateLocked()
	}
	return expired
}

func (a *DemandAggregator) recalculateLocked() {
	var total int64
	for _, member := range a.members {
		if member.demand > math.MaxInt64-total {
			total = math.MaxInt64
			break
		}
		total += member.demand
	}
	a.total = total
}

func (a *DemandAggregator) publishLocked() {
	a.metrics.demand.Set(float64(a.total))
	a.metrics.members.Set(float64(len(a.members)))
	a.demands.Publish(a.total)
}
