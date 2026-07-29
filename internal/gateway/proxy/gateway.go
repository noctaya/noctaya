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

// Package proxy implements the per-model HTTP proxy and demand reporter.
package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/noctaya/noctaya/internal/gateway/demand"
)

type Gateway struct {
	cfg     Config
	backend *url.URL
	proxy   *httputil.ReverseProxy
	sem     chan struct{}
	pending atomic.Int64
	m       *metrics
	probe   *http.Client
	now     func() time.Time
	ctx     context.Context
	cancel  context.CancelFunc

	probeMu    sync.Mutex
	probeAt    time.Time
	probeReady bool
	probeDone  chan struct{}

	leaseMu         sync.Mutex
	leaseActive     bool
	leaseDeadline   time.Time
	readyGraceUntil time.Time
	leaseWatcher    bool

	notifyMu sync.Mutex
	demands  demand.Subscriptions
}

func New(cfg Config) (*Gateway, error) {
	backend, err := url.Parse(cfg.BackendURL)
	if err != nil || backend.Host == "" || (backend.Scheme != "http" && backend.Scheme != "https") {
		return nil, fmt.Errorf("backend URL must be an absolute HTTP URL")
	}
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = 100
	}
	if cfg.ActivationTimeout <= 0 {
		cfg.ActivationTimeout = 5 * time.Minute
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 500 * time.Millisecond
	}
	if cfg.ColdStartMode != ColdStartReject {
		cfg.ColdStartMode = ColdStartKeepalive
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 10 * time.Second
	}
	if cfg.ActivationGracePeriod <= 0 {
		cfg.ActivationGracePeriod = 15 * time.Second
	}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.FlushInterval = -1
	ctx, cancel := context.WithCancel(context.Background())
	return &Gateway{
		cfg:     cfg,
		backend: backend,
		proxy:   proxy,
		sem:     make(chan struct{}, cfg.MaxQueue),
		m:       newMetrics(),
		probe:   &http.Client{Timeout: 2 * time.Second},
		now:     time.Now,
		ctx:     ctx,
		cancel:  cancel,
		demands: demand.NewSubscriptions(),
	}, nil
}

func (g *Gateway) Close() { g.cancel() }
