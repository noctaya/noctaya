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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testDemandToken = "0123456789abcdef0123456789abcdef"

func TestDemandAggregatorAggregatesAndExpiresMembers(t *testing.T) {
	now := time.Unix(100, 0)
	aggregator := NewDemandAggregator(DemandAggregatorConfig{
		MemberTTL: time.Second,
		Now:       func() time.Time { return now },
	})
	updates, unsubscribe := aggregator.SubscribeDemand()
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

func TestDemandAggregatorExpiresDisconnectedReporter(t *testing.T) {
	aggregator := NewDemandAggregator(DemandAggregatorConfig{
		MemberTTL:     40 * time.Millisecond,
		SweepInterval: 5 * time.Millisecond,
		AuthTokenFile: demandTokenFile(t),
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
	aggregator := NewDemandAggregator(DemandAggregatorConfig{AuthTokenFile: demandTokenFile(t)})
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
		request, err := http.NewRequest(http.MethodPost, server.URL+DemandReportPath, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+testDemandToken)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for report %+v", response.StatusCode, report)
		}
	}
}

func TestDemandAggregatorRequiresAuthenticationAndBoundsMembers(t *testing.T) {
	aggregator := NewDemandAggregator(DemandAggregatorConfig{
		AuthTokenFile: demandTokenFile(t),
		MaxMembers:    1,
		MaxDemand:     2,
	})
	server := httptest.NewServer(aggregator.Handler())
	t.Cleanup(server.Close)

	body, err := json.Marshal(demandReport{MemberID: "a", Sequence: 1, Demand: 1})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+DemandReportPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.StatusCode)
	}

	postDemandReport(t, server.URL+DemandReportPath, demandReport{MemberID: "a", Sequence: 1, Demand: 2})

	requestBody, err := json.Marshal(demandReport{MemberID: "b", Sequence: 1, Demand: 1})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+DemandReportPath, bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testDemandToken)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("member-capacity status = %d, want 429", response.StatusCode)
	}

	requestBody, err = json.Marshal(demandReport{MemberID: "a", Sequence: 2, Demand: 3})
	if err != nil {
		t.Fatal(err)
	}
	request, err = http.NewRequest(http.MethodPost, server.URL+DemandReportPath, bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testDemandToken)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("excess-demand status = %d, want 400", response.StatusCode)
	}
}

func TestDemandAggregatorReadinessRequiresAuthenticationMaterial(t *testing.T) {
	for name, config := range map[string]DemandAggregatorConfig{
		"missing": {},
		"valid":   {AuthTokenFile: demandTokenFile(t)},
	} {
		t.Run(name, func(t *testing.T) {
			aggregator := NewDemandAggregator(config)
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()
			aggregator.Handler().ServeHTTP(response, request)
			want := http.StatusServiceUnavailable
			if name == "valid" {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("readiness status = %d, want %d", response.Code, want)
			}
		})
	}
}

func postDemandReport(t *testing.T, endpoint string, report demandReport) {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testDemandToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
}

func demandTokenFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(testDemandToken), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
