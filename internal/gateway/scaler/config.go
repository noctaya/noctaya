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

// Package scaler implements demand aggregation and the KEDA External Push API.
package scaler

const (
	EnvListenAddr           = "NOCTAYA_SCALER_LISTEN_ADDR"
	EnvAggregatorListenAddr = "NOCTAYA_AGGREGATOR_LISTEN_ADDR"
	EnvMaxGatewayMembers    = "NOCTAYA_MAX_GATEWAY_MEMBERS"
	EnvMaxGatewayDemand     = "NOCTAYA_MAX_GATEWAY_DEMAND"
	EnvTLSCertFile          = "NOCTAYA_SCALER_TLS_CERT_FILE"
	EnvTLSKeyFile           = "NOCTAYA_SCALER_TLS_KEY_FILE"
	EnvTLSClientCAFile      = "NOCTAYA_SCALER_TLS_CLIENT_CA_FILE"

	DefaultListenAddr           = ":9090"
	DefaultAggregatorListenAddr = ":9091"
	MetricsPath                 = "/metrics"
)
