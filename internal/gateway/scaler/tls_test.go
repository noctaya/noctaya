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

package scaler_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/noctaya/noctaya/internal/gateway/scaler"
)

func TestExternalScalerServerOptionsFromEnv(t *testing.T) {
	g := NewWithT(t)
	clearScalerTLSEnv(t)

	options, err := scaler.ExternalScalerServerOptionsFromEnv()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(options).To(BeEmpty())

	certFile, keyFile, caFile := writeScalerServerCredentials(t)
	t.Setenv(scaler.EnvTLSCertFile, certFile)
	t.Setenv(scaler.EnvTLSKeyFile, keyFile)
	t.Setenv(scaler.EnvTLSClientCAFile, caFile)
	options, err = scaler.ExternalScalerServerOptionsFromEnv()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(options).To(HaveLen(1))
}

func TestExternalScalerServerOptionsRejectsPartialOrInvalidCredentials(t *testing.T) {
	t.Run("partial configuration", func(t *testing.T) {
		g := NewWithT(t)
		clearScalerTLSEnv(t)
		t.Setenv(scaler.EnvTLSCertFile, "server.crt")
		_, err := scaler.ExternalScalerServerOptionsFromEnv()
		g.Expect(err).To(MatchError(ContainSubstring("must be set together")))
	})

	t.Run("invalid client CA", func(t *testing.T) {
		g := NewWithT(t)
		clearScalerTLSEnv(t)
		certFile, keyFile, caFile := writeScalerServerCredentials(t)
		g.Expect(os.WriteFile(caFile, []byte("not a certificate"), 0o600)).To(Succeed())
		t.Setenv(scaler.EnvTLSCertFile, certFile)
		t.Setenv(scaler.EnvTLSKeyFile, keyFile)
		t.Setenv(scaler.EnvTLSClientCAFile, caFile)
		_, err := scaler.ExternalScalerServerOptionsFromEnv()
		g.Expect(err).To(MatchError("parse external scaler client CA"))
	})
}

func clearScalerTLSEnv(t *testing.T) {
	t.Helper()
	t.Setenv(scaler.EnvTLSCertFile, "")
	t.Setenv(scaler.EnvTLSKeyFile, "")
	t.Setenv(scaler.EnvTLSClientCAFile, "")
}

func writeScalerServerCredentials(t *testing.T) (string, string, string) {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Noctaya test CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "scaler.test"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"scaler.test"},
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader,
		serverTemplate,
		caTemplate,
		&serverKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	caFile := filepath.Join(dir, "ca.crt")
	writePEM(t, certFile, "CERTIFICATE", serverDER)
	writePEM(t, keyFile, "PRIVATE KEY", keyDER)
	writePEM(t, caFile, "CERTIFICATE", caDER)
	return certFile, keyFile, caFile
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}
