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

func (g *Gateway) Pending() int64 { return g.pending.Load() }

// Demand keeps a floor of one while an activation lease or readiness grace is active.
func (g *Gateway) Demand() int64 {
	pending := g.pending.Load()
	if pending > 0 {
		return pending
	}
	now := g.now()
	g.leaseMu.Lock()
	active := (g.leaseActive && now.Before(g.leaseDeadline)) || now.Before(g.readyGraceUntil)
	g.leaseMu.Unlock()
	if active {
		return 1
	}
	return 0
}

func (g *Gateway) changePending(delta int64) {
	g.m.pending.Set(float64(g.pending.Add(delta)))
	g.notifyDemandChange()
}

func (g *Gateway) notifyDemandChange() {
	g.notifyMu.Lock()
	defer g.notifyMu.Unlock()

	demand := g.Demand()
	g.m.demand.Set(float64(demand))
	if g.demands.Publish(demand) {
		g.m.activationEvents.Inc()
	}
}

func (g *Gateway) SubscribeDemand() (<-chan int64, func()) {
	g.notifyMu.Lock()
	channel, unsubscribe := g.demands.Subscribe(g.Demand())
	g.notifyMu.Unlock()
	return channel, unsubscribe
}
