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

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDemandAggregatorAggregatesAndExpiresMembers(t *testing.T) {
	now := time.Unix(100, 0)
	aggregator := NewDemandAggregator(DemandAggregatorConfig{
		MemberTTL: time.Second,
		Now:       func() time.Time { return now },
	})
	updates, unsubscribe := aggregator.subscribeDemand()
	t.Cleanup(unsubscribe)
	if demand := <-updates; demand != 0 {
		t.Fatalf("initial demand = %d, want 0", demand)
	}

	if result := aggregator.update(demandReport{MemberID: "a", Sequence: 1, Demand: 2}, now); result != demandReportAccepted {
		t.Fatalf("first update result = %q, want accepted", result)
	}
	if demand := <-updates; demand != 2 {
		t.Fatalf("demand after first member = %d, want 2", demand)
	}
	if result := aggregator.update(demandReport{MemberID: "b", Sequence: 1, Demand: 3}, now); result != demandReportAccepted {
		t.Fatalf("second update result = %q, want accepted", result)
	}
	if demand := <-updates; demand != 5 {
		t.Fatalf("aggregate demand = %d, want 5", demand)
	}
	if result := aggregator.update(demandReport{MemberID: "a", Sequence: 1, Demand: 9}, now); result != demandReportStale {
		t.Fatalf("stale update result = %q, want stale", result)
	}
	if demand := aggregator.Demand(); demand != 5 {
		t.Fatalf("demand after stale update = %d, want 5", demand)
	}

	now = now.Add(time.Second)
	if demand := aggregator.Demand(); demand != 0 {
		t.Fatalf("demand after member expiry = %d, want 0", demand)
	}
	if demand := <-updates; demand != 0 {
		t.Fatalf("published demand after member expiry = %d, want 0", demand)
	}
}

func TestDemandAggregatorReplacementDoesNotPermanentlyDoubleCount(t *testing.T) {
	now := time.Unix(200, 0)
	aggregator := NewDemandAggregator(DemandAggregatorConfig{
		MemberTTL: 100 * time.Millisecond,
		Now:       func() time.Time { return now },
	})
	aggregator.update(demandReport{MemberID: "old-pod", Sequence: 1, Demand: 2}, now)
	now = now.Add(50 * time.Millisecond)
	aggregator.update(demandReport{MemberID: "new-pod", Sequence: 1, Demand: 1}, now)
	if demand := aggregator.Demand(); demand != 3 {
		t.Fatalf("replacement overlap demand = %d, want 3", demand)
	}

	now = now.Add(50 * time.Millisecond)
	if demand := aggregator.Demand(); demand != 1 {
		t.Fatalf("demand after stale member expiry = %d, want 1", demand)
	}
}

func TestDemandReportersAggregateAndWithdrawOnCancellation(t *testing.T) {
	aggregator := NewDemandAggregator(DemandAggregatorConfig{MemberTTL: time.Second})
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
		Endpoint:          server.URL + DemandReportPath,
		GatewayID:         "gateway-a",
		HeartbeatInterval: 20 * time.Millisecond,
		RequestTimeout:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDemandReporter(first) error = %v", err)
	}
	secondReporter, err := NewDemandReporter(second, DemandReporterConfig{
		Endpoint:          server.URL + DemandReportPath,
		GatewayID:         "gateway-b",
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

func TestDemandAggregatorExpiresDisconnectedReporter(t *testing.T) {
	aggregator := NewDemandAggregator(DemandAggregatorConfig{
		MemberTTL:     40 * time.Millisecond,
		SweepInterval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go aggregator.Run(ctx)

	server := httptest.NewServer(aggregator.Handler())
	t.Cleanup(server.Close)
	postDemandReport(t, server.URL+DemandReportPath, demandReport{
		MemberID: "disconnected", Sequence: 1, Demand: 4,
	})
	waitForDemand(t, aggregator, 4)
	waitForDemand(t, aggregator, 0)
}

func TestDemandAggregatorRejectsInvalidReports(t *testing.T) {
	aggregator := NewDemandAggregator(DemandAggregatorConfig{})
	server := httptest.NewServer(aggregator.Handler())
	t.Cleanup(server.Close)
	for _, report := range []demandReport{
		{Sequence: 1, Demand: 1},
		{MemberID: "gateway", Demand: 1},
		{MemberID: "gateway", Sequence: 1, Demand: -1},
	} {
		body, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.Post(server.URL+DemandReportPath, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for report %+v", response.StatusCode, report)
		}
	}
}

func postDemandReport(t *testing.T, endpoint string, report demandReport) {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
}

func waitForDemand(t *testing.T, aggregator *DemandAggregator, want int64) {
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
