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

package demand_test

import (
	"testing"

	"github.com/noctaya/noctaya/internal/gateway/demand"
)

func TestSubscriptionsPublishLatestDemand(t *testing.T) {
	subscriptions := demand.NewSubscriptions()
	updates, unsubscribe := subscriptions.Subscribe(0)

	if current := <-updates; current != 0 {
		t.Fatalf("initial demand = %d, want 0", current)
	}
	if activated := subscriptions.Publish(1); !activated {
		t.Fatal("0 to 1 transition did not activate")
	}
	if activated := subscriptions.Publish(2); activated {
		t.Fatal("positive demand change unexpectedly activated")
	}
	if current := <-updates; current != 2 {
		t.Fatalf("latest demand = %d, want 2", current)
	}
	if current := subscriptions.Current(); current != 2 {
		t.Fatalf("current demand = %d, want 2", current)
	}

	unsubscribe()
	unsubscribe()
	subscriptions.Publish(3)
	select {
	case current := <-updates:
		t.Fatalf("received demand %d after unsubscribe", current)
	default:
	}
}

func TestSubscriptionsDetectReactivation(t *testing.T) {
	subscriptions := demand.NewSubscriptions()
	subscriptions.Publish(1)
	if activated := subscriptions.Publish(0); activated {
		t.Fatal("transition to zero unexpectedly activated")
	}
	if activated := subscriptions.Publish(1); !activated {
		t.Fatal("second 0 to 1 transition did not activate")
	}
}
