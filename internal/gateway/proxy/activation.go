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
	"io"
	"net/http"
	"time"
)

type activationWaitResult int

const (
	activationReady activationWaitResult = iota
	activationTimedOut
	activationClientClosed
)

// beginActivation keeps demand visible after a reject-mode request has returned.
func (g *Gateway) beginActivation() {
	now := g.now()
	g.leaseMu.Lock()
	g.leaseActive = true
	g.leaseDeadline = now.Add(g.cfg.ActivationTimeout)
	startWatcher := !g.leaseWatcher
	if startWatcher {
		g.leaseWatcher = true
	}
	g.leaseMu.Unlock()
	g.notifyDemandChange()
	if startWatcher {
		go g.watchActivation()
	}
}

func (g *Gateway) watchActivation() {
	for {
		select {
		case <-g.ctx.Done():
			return
		default:
		}

		g.leaseMu.Lock()
		deadline := g.leaseDeadline
		g.leaseMu.Unlock()
		if !g.now().Before(deadline) && g.finishActivation(false) {
			return
		}
		probeCtx, cancel := context.WithDeadline(g.ctx, deadline)
		ready := g.backendReady(probeCtx)
		cancel()
		if ready {
			g.finishActivation(true)
			return
		}
		select {
		case <-g.ctx.Done():
			return
		case <-time.After(g.cfg.RetryInterval):
		}
	}
}

// finishActivation returns false when a newer request renewed an expired lease.
func (g *Gateway) finishActivation(ready bool) bool {
	now := g.now()
	g.leaseMu.Lock()
	if !ready && now.Before(g.leaseDeadline) {
		g.leaseMu.Unlock()
		return false
	}
	g.leaseActive = false
	g.leaseWatcher = false
	if ready {
		g.readyGraceUntil = now.Add(g.cfg.ActivationGracePeriod)
	}
	graceDeadline := g.readyGraceUntil
	g.leaseMu.Unlock()
	g.notifyDemandChange()
	if ready {
		go g.notifyAfterGrace(graceDeadline)
	}
	return true
}

func (g *Gateway) notifyAfterGrace(deadline time.Time) {
	delay := max(time.Duration(0), time.Until(deadline))
	select {
	case <-g.ctx.Done():
	case <-time.After(delay):
		g.notifyDemandChange()
	}
}

// holdWithHeartbeat commits an SSE response and emits heartbeats during activation.
func (g *Gateway) holdWithHeartbeat(
	w http.ResponseWriter,
	request *http.Request,
) (activationWaitResult, bool) {
	if request.Context().Err() != nil {
		return activationClientClosed, false
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return g.waitForBackend(request.Context()), false
	}
	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := request.Context()
	deadline := g.now().Add(g.cfg.ActivationTimeout)
	lastBeat := g.now()
	for {
		if g.backendReady(ctx) {
			return activationReady, true
		}
		if g.now().After(deadline) {
			return activationTimedOut, true
		}
		if g.now().Sub(lastBeat) >= g.cfg.HeartbeatInterval {
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return activationClientClosed, true
			}
			flusher.Flush()
			lastBeat = g.now()
		}
		select {
		case <-ctx.Done():
			return activationClientClosed, true
		case <-time.After(g.cfg.RetryInterval):
		}
	}
}

func (g *Gateway) writeStreamError(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "event: error\ndata: backend activation timeout\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (g *Gateway) waitForBackend(ctx context.Context) activationWaitResult {
	deadline := g.now().Add(g.cfg.ActivationTimeout)
	for {
		if ctx.Err() != nil {
			return activationClientClosed
		}
		if g.backendReady(ctx) {
			return activationReady
		}
		if g.now().After(deadline) {
			return activationTimedOut
		}
		select {
		case <-ctx.Done():
			return activationClientClosed
		case <-time.After(g.cfg.RetryInterval):
		}
	}
}

func (g *Gateway) backendReady(ctx context.Context) bool {
	for {
		now := g.now()
		g.probeMu.Lock()
		if !g.probeAt.IsZero() && now.Sub(g.probeAt) < g.cfg.RetryInterval {
			ready := g.probeReady
			g.probeMu.Unlock()
			return ready
		}
		if done := g.probeDone; done != nil {
			g.probeMu.Unlock()
			select {
			case <-ctx.Done():
				return false
			case <-done:
				continue
			}
		}
		done := make(chan struct{})
		g.probeDone = done
		g.probeMu.Unlock()

		ready := g.probeBackend(ctx)
		g.probeMu.Lock()
		g.probeAt = g.now()
		g.probeReady = ready
		g.probeDone = nil
		close(done)
		g.probeMu.Unlock()
		return ready
	}
}

func (g *Gateway) probeBackend(ctx context.Context) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.backend.String()+"/health", nil)
	if err != nil {
		return false
	}
	response, err := g.probe.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode == http.StatusOK
}
