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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// ExternalScalerServerOptionsFromEnv configures mutual TLS when all scaler TLS
// paths are present. Leaving all paths empty preserves the plaintext endpoint.
func ExternalScalerServerOptionsFromEnv() ([]grpc.ServerOption, error) {
	certFile := os.Getenv(EnvTLSCertFile)
	keyFile := os.Getenv(EnvTLSKeyFile)
	caFile := os.Getenv(EnvTLSClientCAFile)
	if certFile == "" && keyFile == "" && caFile == "" {
		return nil, nil
	}
	if certFile == "" || keyFile == "" || caFile == "" {
		return nil, fmt.Errorf(
			"%s, %s, and %s must be set together",
			EnvTLSCertFile,
			EnvTLSKeyFile,
			EnvTLSClientCAFile,
		)
	}

	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load external scaler server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read external scaler client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse external scaler client CA")
	}

	config := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}
	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(config))}, nil
}
