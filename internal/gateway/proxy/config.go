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
	"os"
	"strconv"
	"time"
)

const (
	EnvBackendURL          = "NOCTAYA_BACKEND_URL"
	EnvMaxQueue            = "NOCTAYA_MAX_QUEUE"
	EnvActivationTimeout   = "NOCTAYA_ACTIVATION_TIMEOUT"
	EnvListenAddr          = "NOCTAYA_LISTEN_ADDR"
	EnvDemandAggregatorURL = "NOCTAYA_DEMAND_AGGREGATOR_URL"
	EnvGatewayID           = "NOCTAYA_GATEWAY_ID"
	EnvColdStartMode       = "NOCTAYA_COLDSTART_MODE"
	EnvHeartbeatInterval   = "NOCTAYA_HEARTBEAT_INTERVAL"
	EnvClientAPIKeyFile    = "NOCTAYA_CLIENT_API_KEY_FILE"

	DefaultListenAddr = ":8080"
	DefaultMaxQueue   = 100
	QueuePath         = "/noctaya/queue"
	MetricsPath       = "/metrics"

	ColdStartKeepalive = "keepalive"
	ColdStartReject    = "reject"
)

type Config struct {
	BackendURL        string
	MaxQueue          int
	ActivationTimeout time.Duration
	RetryInterval     time.Duration
	ColdStartMode     string
	HeartbeatInterval time.Duration
	ClientAPIKeyFile  string
	// ActivationGracePeriod prevents immediate scale-down while reject-mode clients retry.
	ActivationGracePeriod time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		BackendURL:       os.Getenv(EnvBackendURL),
		ColdStartMode:    os.Getenv(EnvColdStartMode),
		ClientAPIKeyFile: os.Getenv(EnvClientAPIKeyFile),
	}
	if value, err := strconv.Atoi(os.Getenv(EnvMaxQueue)); err == nil {
		cfg.MaxQueue = value
	}
	if duration, err := time.ParseDuration(os.Getenv(EnvActivationTimeout)); err == nil {
		cfg.ActivationTimeout = duration
	}
	if duration, err := time.ParseDuration(os.Getenv(EnvHeartbeatInterval)); err == nil {
		cfg.HeartbeatInterval = duration
	}
	return cfg
}
