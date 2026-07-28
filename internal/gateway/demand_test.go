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
	"sync"
	"testing"
)

func TestConcurrentDemandNotificationsConverge(t *testing.T) {
	gateway, err := New(Config{BackendURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gateway.Close)

	updates, unsubscribe := gateway.subscribeDemand()
	t.Cleanup(unsubscribe)
	if active := <-updates; active {
		t.Fatal("initial demand is active")
	}

	const (
		workers     = 32
		transitions = 200
	)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range transitions {
				gateway.pending.Add(1)
				gateway.notifyDemandChange()
				gateway.pending.Add(-1)
				gateway.notifyDemandChange()
			}
		})
	}
	wg.Wait()
	gateway.notifyDemandChange()

	if demand := gateway.Demand(); demand != 0 {
		t.Fatalf("Demand() = %d, want 0", demand)
	}
	gateway.subMu.Lock()
	lastActive := gateway.lastActive
	gateway.subMu.Unlock()
	if lastActive {
		t.Fatal("published demand did not converge to inactive")
	}

	select {
	case active := <-updates:
		if active {
			t.Fatal("last queued demand update is active")
		}
	default:
	}
}
