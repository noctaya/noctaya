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

import "github.com/prometheus/client_golang/prometheus"

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

func (g *Gateway) ScalerStreamConnected() { g.m.scalerStreams.Inc() }

func (g *Gateway) ScalerStreamDisconnected() { g.m.scalerStreams.Dec() }

func (g *Gateway) recordDemandReport(result string) {
	g.m.demandReports.WithLabelValues(result).Inc()
}
