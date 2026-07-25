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

package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/noctaya/noctaya/internal/gateway"
)

// stubBackend is a fake vLLM: /health reflects a toggle, everything else echoes.
type stubBackend struct {
	ready   atomic.Bool
	release chan struct{} // when non-nil, /v1 handlers block until it closes
}

func (s *stubBackend) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if s.ready.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		if s.release != nil {
			<-s.release
		}
		_, _ = io.WriteString(w, "ok")
	})
	return mux
}

func newGateway(t *testing.T, backendURL string, maxQueue int, timeout time.Duration) *gateway.Gateway {
	t.Helper()
	g := NewWithT(t)
	gw, err := gateway.New(gateway.Config{
		BackendURL:        backendURL,
		MaxQueue:          maxQueue,
		ActivationTimeout: timeout,
		RetryInterval:     5 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gw.Close)
	return gw
}

func TestForwardsWhenBackendReady(t *testing.T) {
	g := NewWithT(t)
	be := &stubBackend{}
	be.ready.Store(true)
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	fe := httptest.NewServer(newGateway(t, srv.URL, 10, time.Second).Handler())
	defer fe.Close()

	resp, err := http.Post(fe.URL+"/v1/chat/completions", "application/json", nil)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	g.Expect(string(body)).To(Equal("ok"))
}

func TestHoldsThenForwardsOnColdStart(t *testing.T) {
	g := NewWithT(t)
	be := &stubBackend{} // starts not ready
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	go func() {
		time.Sleep(40 * time.Millisecond)
		be.ready.Store(true)
	}()

	fe := httptest.NewServer(newGateway(t, srv.URL, 10, 2*time.Second).Handler())
	defer fe.Close()

	resp, err := http.Post(fe.URL+"/v1/x", "application/json", nil)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
}

func TestReturns503OnActivationTimeout(t *testing.T) {
	g := NewWithT(t)
	be := &stubBackend{} // never ready
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	fe := httptest.NewServer(newGateway(t, srv.URL, 10, 60*time.Millisecond).Handler())
	defer fe.Close()

	resp, err := http.Post(fe.URL+"/v1/x", "application/json", nil)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	g.Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
	g.Expect(resp.Header.Get("Retry-After")).NotTo(BeEmpty())
}

func TestBackpressureReturns429WhenQueueFull(t *testing.T) {
	g := NewWithT(t)
	release := make(chan struct{})
	be := &stubBackend{release: release}
	be.ready.Store(true)
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	fe := httptest.NewServer(newGateway(t, srv.URL, 1, time.Second).Handler())
	defer fe.Close()

	go func() {
		resp, err := http.Post(fe.URL+"/v1/hold", "application/json", nil)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	g.Eventually(func() int64 { return queuePending(fe.URL) }).Should(Equal(int64(1)))

	resp, err := http.Post(fe.URL+"/v1/second", "application/json", nil)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	g.Expect(resp.StatusCode).To(Equal(http.StatusTooManyRequests))

	close(release)
}

func TestExposesPrometheusMetrics(t *testing.T) {
	g := NewWithT(t)
	be := &stubBackend{}
	be.ready.Store(true)
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	fe := httptest.NewServer(newGateway(t, srv.URL, 10, time.Second).Handler())
	defer fe.Close()

	resp, err := http.Post(fe.URL+"/v1/x", "application/json", nil)
	g.Expect(err).NotTo(HaveOccurred())
	_ = resp.Body.Close()

	m, err := http.Get(fe.URL + gateway.MetricsPath)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = m.Body.Close() }()
	body, _ := io.ReadAll(m.Body)
	g.Expect(string(body)).To(ContainSubstring("noctaya_gateway_requests_total"))
	g.Expect(string(body)).To(ContainSubstring("noctaya_gateway_activation_wait_seconds"))
	g.Expect(string(body)).To(ContainSubstring("noctaya_gateway_demand"))
}

func TestKeepaliveStreamsHeartbeatsThenResponse(t *testing.T) {
	g := NewWithT(t)
	be := &stubBackend{} // starts not ready
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	go func() {
		time.Sleep(60 * time.Millisecond)
		be.ready.Store(true)
	}()

	gw, err := gateway.New(gateway.Config{
		BackendURL:        srv.URL,
		MaxQueue:          10,
		ActivationTimeout: 2 * time.Second,
		RetryInterval:     5 * time.Millisecond,
		ColdStartMode:     gateway.ColdStartKeepalive,
		HeartbeatInterval: 10 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())
	fe := httptest.NewServer(gw.Handler())
	defer fe.Close()

	resp, err := http.Post(fe.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"stream":true}`))
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	g.Expect(resp.Header.Get("Content-Type")).To(Equal("text/event-stream"))

	body, _ := io.ReadAll(resp.Body)
	g.Expect(string(body)).To(ContainSubstring(": heartbeat"))
	g.Expect(string(body)).To(ContainSubstring("ok"))
}

func TestKeepaliveTimeoutMetricsMatchCommittedResponse(t *testing.T) {
	g := NewWithT(t)
	gw, err := gateway.New(gateway.Config{
		BackendURL:        "http://127.0.0.1:1",
		MaxQueue:          10,
		ActivationTimeout: 30 * time.Millisecond,
		RetryInterval:     5 * time.Millisecond,
		ColdStartMode:     gateway.ColdStartKeepalive,
		HeartbeatInterval: 5 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"stream":true}`))
	response := httptest.NewRecorder()
	gw.Handler().ServeHTTP(response, request)
	g.Expect(response.Code).To(Equal(http.StatusOK))
	g.Expect(response.Body.String()).To(ContainSubstring("backend activation timeout"))

	metricsRequest := httptest.NewRequest(http.MethodGet, gateway.MetricsPath, nil)
	metricsResponse := httptest.NewRecorder()
	gw.Handler().ServeHTTP(metricsResponse, metricsRequest)
	metricsText := metricsResponse.Body.String()
	g.Expect(metricsText).To(ContainSubstring(`noctaya_gateway_requests_total{code="200"} 1`))
	g.Expect(metricsText).NotTo(ContainSubstring(`noctaya_gateway_requests_total{code="503"}`))
	g.Expect(metricsText).To(ContainSubstring(`noctaya_gateway_rejections_total{reason="activation_timeout"} 1`))
}

func TestCanceledColdRequestIsNotActivationTimeout(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "streaming", body: `{"stream":true}`},
		{name: "non-streaming", body: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			gw := newGateway(t, "http://127.0.0.1:1", 10, time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
				strings.NewReader(tc.body)).WithContext(ctx)
			response := httptest.NewRecorder()
			gw.Handler().ServeHTTP(response, req)

			metricsRequest := httptest.NewRequest(http.MethodGet, gateway.MetricsPath, nil)
			metricsResponse := httptest.NewRecorder()
			gw.Handler().ServeHTTP(metricsResponse, metricsRequest)
			g.Expect(metricsResponse.Body.String()).NotTo(ContainSubstring(`reason="activation_timeout"`))
		})
	}
}

func TestRejectModeReturns503AndKeepsDemand(t *testing.T) {
	g := NewWithT(t)
	be := &stubBackend{} // never ready
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	gw, err := gateway.New(gateway.Config{
		BackendURL:        srv.URL,
		MaxQueue:          10,
		ActivationTimeout: time.Second,
		RetryInterval:     5 * time.Millisecond,
		ColdStartMode:     gateway.ColdStartReject,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gw.Close)
	fe := httptest.NewServer(gw.Handler())
	defer fe.Close()

	resp, err := http.Post(fe.URL+"/v1/x", "application/json", nil)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	g.Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
	g.Expect(resp.Header.Get("Retry-After")).NotTo(BeEmpty())

	g.Expect(queuePending(fe.URL)).To(Equal(int64(1)))
}

func TestActivationLeaseEndsAfterBackendReadyGrace(t *testing.T) {
	g := NewWithT(t)
	be := &stubBackend{}
	srv := httptest.NewServer(be.handler())
	defer srv.Close()

	gw, err := gateway.New(gateway.Config{
		BackendURL:            srv.URL,
		ColdStartMode:         gateway.ColdStartReject,
		ActivationTimeout:     time.Second,
		ActivationGracePeriod: 80 * time.Millisecond,
		RetryInterval:         5 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gw.Close)

	response := httptest.NewRecorder()
	gw.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/x", nil))
	g.Expect(response.Code).To(Equal(http.StatusServiceUnavailable))
	g.Expect(gw.Demand()).To(Equal(int64(1)))

	be.ready.Store(true)
	g.Consistently(gw.Demand, 40*time.Millisecond, 5*time.Millisecond).Should(Equal(int64(1)))
	g.Eventually(gw.Demand, time.Second, 5*time.Millisecond).Should(Equal(int64(0)))
}

func queuePending(base string) int64 {
	resp, err := http.Get(base + gateway.QueuePath)
	if err != nil {
		return -1
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Pending int64 `json:"pending"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return -1
	}
	return body.Pending
}
