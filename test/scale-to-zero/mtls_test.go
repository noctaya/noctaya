//go:build e2e

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

package scaletozero

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func createExternalScalerTLSSecrets(serviceName string) {
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Noctaya E2E CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(
		rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey,
	)
	Expect(err).NotTo(HaveOccurred())

	serverCert, serverKey := issueCertificate(
		caTemplate,
		caKey,
		big.NewInt(2),
		"noctaya-external-scaler",
		[]string{
			serviceName + "-scaler." + namespace + ".svc",
			serviceName + "-scaler." + namespace + ".svc.cluster.local",
		},
		x509.ExtKeyUsageServerAuth,
	)
	clientCert, clientKey := issueCertificate(
		caTemplate,
		caKey,
		big.NewInt(3),
		"keda-external-push",
		nil,
		x509.ExtKeyUsageClientAuth,
	)

	dir := GinkgoT().TempDir()
	caFile := filepath.Join(dir, "ca.crt")
	serverCertFile := filepath.Join(dir, "server.crt")
	serverKeyFile := filepath.Join(dir, "server.key")
	clientCertFile := filepath.Join(dir, "client.crt")
	clientKeyFile := filepath.Join(dir, "client.key")
	writeTestPEM(caFile, "CERTIFICATE", caDER)
	writeTestPEM(serverCertFile, "CERTIFICATE", serverCert)
	writeTestPEM(serverKeyFile, "PRIVATE KEY", serverKey)
	writeTestPEM(clientCertFile, "CERTIFICATE", clientCert)
	writeTestPEM(clientKeyFile, "PRIVATE KEY", clientKey)

	mustKubectl(
		"create", "secret", "generic", "noctaya-external-scaler-server",
		"-n", namespace,
		"--from-file=tls.crt="+serverCertFile,
		"--from-file=tls.key="+serverKeyFile,
		"--from-file=ca.crt="+caFile,
	)
	mustKubectl(
		"create", "secret", "generic", "noctaya-external-scaler-client",
		"-n", namespace,
		"--from-file=tls.crt="+clientCertFile,
		"--from-file=tls.key="+clientKeyFile,
		"--from-file=ca.crt="+caFile,
	)
}

func issueCertificate(
	ca *x509.Certificate,
	caKey *ecdsa.PrivateKey,
	serial *big.Int,
	commonName string,
	dnsNames []string,
	usage x509.ExtKeyUsage,
) ([]byte, []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     dnsNames,
	}
	certificate, err := x509.CreateCertificate(
		rand.Reader, template, ca, &key.PublicKey, caKey,
	)
	Expect(err).NotTo(HaveOccurred())
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	Expect(err).NotTo(HaveOccurred())
	return certificate, keyDER
}

func writeTestPEM(path, blockType string, der []byte) {
	Expect(os.WriteFile(
		path,
		pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}),
		0o600,
	)).To(Succeed())
}
