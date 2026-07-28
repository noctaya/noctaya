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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultReportHeartbeat = 2 * time.Second
	defaultReportTimeout   = time.Second
)

type DemandReporterConfig struct {
	Endpoint          string
	GatewayID         string
	HeartbeatInterval time.Duration
	RequestTimeout    time.Duration
	Client            *http.Client
}

type DemandReporter struct {
	source            *Gateway
	endpoint          string
	memberID          string
	heartbeatInterval time.Duration
	requestTimeout    time.Duration
	client            *http.Client
	sequence          uint64
}

func NewDemandReporter(source *Gateway, cfg DemandReporterConfig) (*DemandReporter, error) {
	if source == nil {
		return nil, fmt.Errorf("gateway is required")
	}
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("demand aggregator endpoint must be an absolute HTTP URL")
	}
	if cfg.GatewayID == "" {
		return nil, fmt.Errorf("gateway ID is required")
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = defaultReportHeartbeat
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultReportTimeout
	}
	if cfg.Client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		cfg.Client = &http.Client{Transport: transport}
	}
	suffix := make([]byte, 12)
	if _, err := rand.Read(suffix); err != nil {
		return nil, fmt.Errorf("generate demand reporter identity: %w", err)
	}
	return &DemandReporter{
		source:            source,
		endpoint:          strings.TrimRight(parsed.String(), "/"),
		memberID:          cfg.GatewayID + "/" + hex.EncodeToString(suffix),
		heartbeatInterval: cfg.HeartbeatInterval,
		requestTimeout:    cfg.RequestTimeout,
		client:            cfg.Client,
	}, nil
}

func (r *DemandReporter) Run(ctx context.Context) {
	updates, unsubscribe := r.source.subscribeDemand()
	defer unsubscribe()
	ticker := time.NewTicker(r.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case demand := <-updates:
			r.send(ctx, demand)
		case <-ticker.C:
			r.send(ctx, r.source.Demand())
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), r.requestTimeout)
			r.send(shutdownCtx, 0)
			cancel()
			return
		}
	}
}

func (r *DemandReporter) send(ctx context.Context, demand int64) {
	r.sequence++
	body, err := json.Marshal(demandReport{
		MemberID: r.memberID,
		Sequence: r.sequence,
		Demand:   demand,
	})
	if err != nil {
		r.source.recordDemandReport("error")
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		r.source.recordDemandReport("error")
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		r.source.recordDemandReport("error")
		return
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		r.source.recordDemandReport("error")
		return
	}
	r.source.recordDemandReport("success")
}
