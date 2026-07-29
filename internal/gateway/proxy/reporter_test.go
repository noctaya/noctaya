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
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noctaya/noctaya/internal/gateway/scaler"
)

func TestDemandReportersAggregateAndWithdrawOnCancellation(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	aggregator := scaler.NewDemandAggregator(scaler.DemandAggregatorConfig{
		MemberTTL:     time.Second,
		AuthTokenFile: tokenFile,
	})
	server := httptest.NewServer(aggregator.Handler())
	t.Cleanup(server.Close)

	first, err := New(Config{BackendURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New(first) error = %v", err)
	}
	t.Cleanup(first.Close)
	second, err := New(Config{BackendURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New(second) error = %v", err)
	}
	t.Cleanup(second.Close)

	firstReporter, err := NewDemandReporter(first, DemandReporterConfig{
		Endpoint:          server.URL + scaler.DemandReportPath,
		GatewayID:         "gateway-a",
		AuthTokenFile:     tokenFile,
		HeartbeatInterval: 20 * time.Millisecond,
		RequestTimeout:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDemandReporter(first) error = %v", err)
	}
	secondReporter, err := NewDemandReporter(second, DemandReporterConfig{
		Endpoint:          server.URL + scaler.DemandReportPath,
		GatewayID:         "gateway-b",
		AuthTokenFile:     tokenFile,
		HeartbeatInterval: 20 * time.Millisecond,
		RequestTimeout:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDemandReporter(second) error = %v", err)
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	go func() {
		firstReporter.Run(firstCtx)
		close(firstDone)
	}()
	go func() {
		secondReporter.Run(secondCtx)
		close(secondDone)
	}()

	first.changePending(1)
	second.changePending(2)
	waitForDemand(t, aggregator, 3)

	cancelFirst()
	<-firstDone
	waitForDemand(t, aggregator, 2)

	second.changePending(-2)
	waitForDemand(t, aggregator, 0)
	cancelSecond()
	<-secondDone
}

func waitForDemand(t *testing.T, aggregator *scaler.DemandAggregator, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if aggregator.Demand() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("demand = %d, want %d", aggregator.Demand(), want)
}
