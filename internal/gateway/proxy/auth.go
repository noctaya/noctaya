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
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const (
	bearerPrefix    = "Bearer "
	bearerChallenge = `Bearer realm="noctaya"`
)

func readClientAPIKey(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read client API key: %w", err)
	}
	key := strings.TrimSpace(string(value))
	if key == "" {
		return "", fmt.Errorf("client API key must not be empty")
	}
	return key, nil
}

func (g *Gateway) authorize(w http.ResponseWriter, request *http.Request) bool {
	if g.cfg.ClientAPIKeyFile == "" {
		return true
	}
	expected, err := readClientAPIKey(g.cfg.ClientAPIKeyFile)
	if err != nil {
		g.m.rejections.WithLabelValues("authentication_unavailable").Inc()
		g.m.requests.WithLabelValues("503").Inc()
		http.Error(w, "gateway authentication unavailable", http.StatusServiceUnavailable)
		return false
	}
	provided, found := strings.CutPrefix(request.Header.Get("Authorization"), bearerPrefix)
	if !found || !apiKeysEqual(provided, expected) {
		g.m.rejections.WithLabelValues("unauthorized").Inc()
		g.m.requests.WithLabelValues("401").Inc()
		w.Header().Set("WWW-Authenticate", bearerChallenge)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	request.Header.Del("Authorization")
	return true
}

func apiKeysEqual(provided, expected string) bool {
	providedDigest := sha256.Sum256([]byte(provided))
	expectedDigest := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) == 1
}
