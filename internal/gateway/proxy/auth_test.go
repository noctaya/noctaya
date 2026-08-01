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

package proxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/noctaya/noctaya/internal/gateway/proxy"
)

func TestClientAPIKeyAuthenticationAndRotation(t *testing.T) {
	g := NewWithT(t)
	keyFile := filepath.Join(t.TempDir(), "api-key")
	g.Expect(os.WriteFile(keyFile, []byte("first-key\n"), 0o600)).To(Succeed())

	var forwardedAuthorization string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		forwardedAuthorization = request.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()

	gateway, err := proxy.New(proxy.Config{
		BackendURL:       backend.URL,
		ClientAPIKeyFile: keyFile,
		RetryInterval:    5 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gateway.Close)
	frontend := httptest.NewServer(gateway.Handler())
	defer frontend.Close()

	assertUnauthorized := func(key string) {
		request, err := http.NewRequest(http.MethodPost, frontend.URL+"/v1/chat/completions", nil)
		g.Expect(err).NotTo(HaveOccurred())
		if key != "" {
			request.Header.Set("Authorization", "Bearer "+key)
		}
		response, err := http.DefaultClient.Do(request)
		g.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = response.Body.Close() }()
		g.Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
		g.Expect(response.Header.Get("WWW-Authenticate")).To(Equal(`Bearer realm="noctaya"`))
	}
	assertAuthorized := func(key string) {
		request, err := http.NewRequest(http.MethodPost, frontend.URL+"/v1/chat/completions", nil)
		g.Expect(err).NotTo(HaveOccurred())
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := http.DefaultClient.Do(request)
		g.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = response.Body.Close() }()
		g.Expect(response.StatusCode).To(Equal(http.StatusOK))
		g.Expect(forwardedAuthorization).To(BeEmpty(), "the gateway credential must not reach the backend")
	}

	assertUnauthorized("")
	assertUnauthorized("wrong-key")
	assertAuthorized("first-key")

	g.Expect(os.WriteFile(keyFile, []byte("rotated-key\n"), 0o600)).To(Succeed())
	assertUnauthorized("first-key")
	assertAuthorized("rotated-key")
}

func TestClientAuthenticationLeavesOperationalEndpointsOpen(t *testing.T) {
	g := NewWithT(t)
	keyFile := filepath.Join(t.TempDir(), "api-key")
	g.Expect(os.WriteFile(keyFile, []byte("test-key"), 0o600)).To(Succeed())

	gateway, err := proxy.New(proxy.Config{
		BackendURL:       "http://127.0.0.1:1",
		ClientAPIKeyFile: keyFile,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gateway.Close)

	for _, path := range []string{"/healthz", proxy.MetricsPath, proxy.QueuePath} {
		response := httptest.NewRecorder()
		gateway.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		g.Expect(response.Code).To(Equal(http.StatusOK), path)
	}
}

func TestAuthorizedRequestsPreserveAdmissionAndCancellation(t *testing.T) {
	g := NewWithT(t)
	keyFile := filepath.Join(t.TempDir(), "api-key")
	g.Expect(os.WriteFile(keyFile, []byte("test-key"), 0o600)).To(Succeed())

	release := make(chan struct{})
	backendState := &stubBackend{release: release}
	backendState.ready.Store(true)
	backend := httptest.NewServer(backendState.handler())
	defer backend.Close()
	gateway, err := proxy.New(proxy.Config{
		BackendURL:       backend.URL,
		ClientAPIKeyFile: keyFile,
		MaxQueue:         1,
		RetryInterval:    5 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gateway.Close)
	frontend := httptest.NewServer(gateway.Handler())
	defer frontend.Close()

	send := func(ctx context.Context, key string) (*http.Response, error) {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			frontend.URL+"/v1/chat/completions",
			nil,
		)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+key)
		return http.DefaultClient.Do(request)
	}

	firstDone := make(chan struct{})
	go func() {
		response, err := send(context.Background(), "test-key")
		if err == nil {
			_ = response.Body.Close()
		}
		close(firstDone)
	}()
	g.Eventually(func() int64 { return queuePending(frontend.URL) }).
		Should(Equal(int64(1)))

	response, err := send(context.Background(), "test-key")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(response.StatusCode).To(Equal(http.StatusTooManyRequests))
	_ = response.Body.Close()

	response, err = send(context.Background(), "wrong-key")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
	_ = response.Body.Close()
	close(release)
	g.Eventually(firstDone).Should(BeClosed())

	canceledGateway, err := proxy.New(proxy.Config{
		BackendURL:       "http://127.0.0.1:1",
		ClientAPIKeyFile: keyFile,
		RetryInterval:    5 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(canceledGateway.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).
		WithContext(ctx)
	request.Header.Set("Authorization", "Bearer test-key")
	recorder := httptest.NewRecorder()
	canceledGateway.Handler().ServeHTTP(recorder, request)

	metrics := httptest.NewRecorder()
	canceledGateway.Handler().ServeHTTP(
		metrics,
		httptest.NewRequest(http.MethodGet, proxy.MetricsPath, nil),
	)
	g.Expect(metrics.Body.String()).NotTo(ContainSubstring(`reason="unauthorized"`))
	g.Expect(metrics.Body.String()).NotTo(ContainSubstring(`reason="activation_timeout"`))
}

func TestClientAuthenticationFailsClosedWhenKeyIsUnavailable(t *testing.T) {
	g := NewWithT(t)
	keyFile := filepath.Join(t.TempDir(), "api-key")
	g.Expect(os.WriteFile(keyFile, []byte("test-key"), 0o600)).To(Succeed())

	gateway, err := proxy.New(proxy.Config{
		BackendURL:       "http://127.0.0.1:1",
		ClientAPIKeyFile: keyFile,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gateway.Close)
	g.Expect(os.Remove(keyFile)).To(Succeed())

	response := httptest.NewRecorder()
	gateway.Handler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil),
	)
	g.Expect(response.Code).To(Equal(http.StatusServiceUnavailable))
	g.Expect(response.Header().Get("WWW-Authenticate")).To(BeEmpty())
}

func TestClientAuthenticationRejectsInvalidInitialKeyFile(t *testing.T) {
	g := NewWithT(t)
	path := filepath.Join(t.TempDir(), "missing")
	_, err := proxy.New(proxy.Config{
		BackendURL:       "http://127.0.0.1:1",
		ClientAPIKeyFile: path,
	})
	g.Expect(err).To(MatchError(ContainSubstring("read client API key")))
}
