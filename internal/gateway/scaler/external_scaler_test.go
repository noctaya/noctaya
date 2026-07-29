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

package scaler_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	externalscaler "github.com/noctaya/noctaya/internal/gateway/externalscaler"
	"github.com/noctaya/noctaya/internal/gateway/proxy"
	"github.com/noctaya/noctaya/internal/gateway/scaler"
)

const bufferSize = 1024 * 1024

func scalerRef() *externalscaler.ScaledObjectRef {
	return &externalscaler.ScaledObjectRef{
		Name:      "qwen",
		Namespace: "ai",
		ScalerMetadata: map[string]string{
			"metricName":  "pending",
			"targetValue": "3",
		},
	}
}

func newScalerClient(t *testing.T, gateway *proxy.Gateway) externalscaler.ExternalScalerClient {
	t.Helper()
	return newScalerClientWithRegistrar(t, func(server *grpc.Server) {
		scaler.RegisterExternalScalerServer(server, gateway)
	})
}

func newAggregatorScalerClient(
	t *testing.T,
	aggregator *scaler.DemandAggregator,
) externalscaler.ExternalScalerClient {
	t.Helper()
	return newScalerClientWithRegistrar(t, func(server *grpc.Server) {
		scaler.RegisterExternalScalerServer(server, aggregator)
	})
}

func newScalerClientWithRegistrar(
	t *testing.T,
	register func(*grpc.Server),
) externalscaler.ExternalScalerClient {
	t.Helper()
	listener := bufconn.Listen(bufferSize)
	server := grpc.NewServer()
	register(server)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///noctaya-scaler",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	})
	return externalscaler.NewExternalScalerClient(conn)
}

func TestExternalScalerReportsAggregateGatewayDemand(t *testing.T) {
	g := NewWithT(t)
	token := "0123456789abcdef0123456789abcdef"
	tokenFile := filepath.Join(t.TempDir(), "token")
	g.Expect(os.WriteFile(tokenFile, []byte(token), 0o600)).To(Succeed())
	aggregator := scaler.NewDemandAggregator(scaler.DemandAggregatorConfig{AuthTokenFile: tokenFile})
	server := httptest.NewServer(aggregator.Handler())
	t.Cleanup(server.Close)
	for _, report := range []string{
		`{"memberID":"gateway-a","sequence":1,"demand":2}`,
		`{"memberID":"gateway-b","sequence":1,"demand":3}`,
	} {
		request, err := http.NewRequest(
			http.MethodPost,
			server.URL+scaler.DemandReportPath,
			strings.NewReader(report),
		)
		g.Expect(err).NotTo(HaveOccurred())
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		g.Expect(err).NotTo(HaveOccurred())
		_ = response.Body.Close()
		g.Expect(response.StatusCode).To(Equal(http.StatusNoContent))
	}

	client := newAggregatorScalerClient(t, aggregator)
	active, err := client.IsActive(context.Background(), scalerRef())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(active.GetResult()).To(BeTrue())
	metrics, err := client.GetMetrics(context.Background(), &externalscaler.GetMetricsRequest{
		ScaledObjectRef: scalerRef(), MetricName: "pending",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(metrics.GetMetricValues()[0].GetMetricValueFloat()).To(Equal(float64(5)))
}

func TestExternalScalerUnaryContract(t *testing.T) {
	g := NewWithT(t)
	gateway, err := proxy.New(proxy.Config{BackendURL: "http://127.0.0.1:1"})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gateway.Close)
	client := newScalerClient(t, gateway)

	active, err := client.IsActive(context.Background(), scalerRef())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(active.GetResult()).To(BeFalse())

	spec, err := client.GetMetricSpec(context.Background(), scalerRef())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(spec.GetMetricSpecs()).To(HaveLen(1))
	g.Expect(spec.GetMetricSpecs()[0].GetMetricName()).To(Equal("pending"))
	g.Expect(spec.GetMetricSpecs()[0].GetTargetSizeFloat()).To(Equal(float64(3)))

	metrics, err := client.GetMetrics(context.Background(), &externalscaler.GetMetricsRequest{
		ScaledObjectRef: scalerRef(), MetricName: "pending",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(metrics.GetMetricValues()).To(HaveLen(1))
	g.Expect(metrics.GetMetricValues()[0].GetMetricValueFloat()).To(Equal(float64(0)))
}

func TestExternalScalerStreamsActivationAndReplaysCurrentState(t *testing.T) {
	g := NewWithT(t)
	gateway, err := proxy.New(proxy.Config{
		BackendURL:        "http://127.0.0.1:1",
		ColdStartMode:     proxy.ColdStartReject,
		ActivationTimeout: 80 * time.Millisecond,
		RetryInterval:     5 * time.Millisecond,
	})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gateway.Close)
	client := newScalerClient(t, gateway)

	ctx := t.Context()
	stream, err := client.StreamIsActive(ctx, scalerRef())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(receiveActive(t, stream)).To(BeFalse(), "a stream must immediately receive current state")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	gateway.Handler().ServeHTTP(response, request)
	g.Expect(response.Code).To(Equal(http.StatusServiceUnavailable))
	g.Expect(receiveActive(t, stream)).To(BeTrue())

	reconnected, err := client.StreamIsActive(ctx, scalerRef())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(receiveActive(t, reconnected)).To(BeTrue(), "reconnections must not wait for another transition")

	metrics, err := client.GetMetrics(ctx, &externalscaler.GetMetricsRequest{
		ScaledObjectRef: scalerRef(), MetricName: "pending",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(metrics.GetMetricValues()[0].GetMetricValueFloat()).To(Equal(float64(1)))

	g.Expect(receiveActive(t, stream)).To(BeFalse(), "the activation lease must expire")
	g.Expect(receiveActive(t, reconnected)).To(BeFalse(), "all connected streams must receive transitions")
}

func TestExternalScalerRejectsInvalidMetadata(t *testing.T) {
	g := NewWithT(t)
	gateway, err := proxy.New(proxy.Config{BackendURL: "http://127.0.0.1:1"})
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(gateway.Close)
	client := newScalerClient(t, gateway)

	for _, target := range []string{"zero", "NaN", "+Inf", "0", "-1"} {
		_, err = client.GetMetricSpec(context.Background(), &externalscaler.ScaledObjectRef{
			ScalerMetadata: map[string]string{"targetValue": target},
		})
		g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument), target)
	}
}

type activeReceiver interface {
	Recv() (*externalscaler.IsActiveResponse, error)
}

func receiveActive(t *testing.T, stream activeReceiver) bool {
	t.Helper()
	type result struct {
		response *externalscaler.IsActiveResponse
		err      error
	}
	results := make(chan result, 1)
	go func() {
		response, err := stream.Recv()
		results <- result{response: response, err: err}
	}()
	select {
	case result := <-results:
		NewWithT(t).Expect(result.err).NotTo(HaveOccurred())
		return result.response.GetResult()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for external scaler stream event")
		return false
	}
}
