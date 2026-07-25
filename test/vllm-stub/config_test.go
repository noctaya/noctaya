/*
Copyright 2026 The Hearth Authors.

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
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name         string
		startupDelay string
		tokenCount   string
		tokenDelay   string
		want         Config
		wantError    string
	}{
		{
			name: "defaults",
			want: Config{TokenDelay: defaultTokenDelay},
		},
		{
			name:         "custom",
			startupDelay: "15s",
			tokenCount:   "20",
			tokenDelay:   "25ms",
			want: Config{
				StartupDelay: 15 * time.Second,
				TokenCount:   20,
				TokenDelay:   25 * time.Millisecond,
			},
		},
		{name: "invalid startup delay", startupDelay: "soon", wantError: "STUB_STARTUP_DELAY"},
		{name: "negative startup delay", startupDelay: "-1s", wantError: "STUB_STARTUP_DELAY"},
		{name: "invalid token count", tokenCount: "many", wantError: "STUB_TOKEN_COUNT"},
		{name: "zero token count", tokenCount: "0", wantError: "STUB_TOKEN_COUNT"},
		{name: "invalid token delay", tokenDelay: "later", wantError: "STUB_TOKEN_DELAY"},
		{name: "negative token delay", tokenDelay: "-1ms", wantError: "STUB_TOKEN_DELAY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("STUB_STARTUP_DELAY", tt.startupDelay)
			t.Setenv("STUB_TOKEN_COUNT", tt.tokenCount)
			t.Setenv("STUB_TOKEN_DELAY", tt.tokenDelay)

			got, err := ConfigFromEnv()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("ConfigFromEnv() error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConfigFromEnv() error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ConfigFromEnv() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
