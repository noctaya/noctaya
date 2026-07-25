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

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const defaultTokenDelay = 50 * time.Millisecond

func ConfigFromEnv() (Config, error) {
	cfg := Config{TokenDelay: defaultTokenDelay}
	if value := os.Getenv("STUB_STARTUP_DELAY"); value != "" {
		delay, err := time.ParseDuration(value)
		if err != nil || delay < 0 {
			return Config{}, fmt.Errorf("STUB_STARTUP_DELAY must be a non-negative duration")
		}
		cfg.StartupDelay = delay
	}
	if value := os.Getenv("STUB_TOKEN_COUNT"); value != "" {
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 {
			return Config{}, fmt.Errorf("STUB_TOKEN_COUNT must be a positive integer")
		}
		cfg.TokenCount = count
	}
	if value := os.Getenv("STUB_TOKEN_DELAY"); value != "" {
		delay, err := time.ParseDuration(value)
		if err != nil || delay < 0 {
			return Config{}, fmt.Errorf("STUB_TOKEN_DELAY must be a non-negative duration")
		}
		cfg.TokenDelay = delay
	}
	return cfg, nil
}

func main() {
	addr := os.Getenv("STUB_LISTEN_ADDR")
	if addr == "" {
		addr = ":8000"
	}
	cfg, err := ConfigFromEnv()
	if err != nil {
		log.Fatalf("Invalid stub configuration: %v", err)
	}
	s := New(cfg)
	log.Printf("vllm-stub listening on %s (startupDelay=%v tokens=%d)", addr, s.cfg.StartupDelay, s.cfg.TokenCount)
	if err := http.ListenAndServe(addr, s.Handler()); err != nil { //nolint:gosec // G114: test-only stub
		log.Fatalf("vllm-stub server failed: %v", err)
	}
}
