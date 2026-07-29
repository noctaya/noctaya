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

package demand

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDemandAuthenticationToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	token := "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadAuthToken(path)
	if err != nil {
		t.Fatalf("ReadAuthToken() error = %v", err)
	}
	if got != token {
		t.Fatalf("ReadAuthToken() = %q, want %q", got, token)
	}
	if !Authorized(AuthorizationValue(token), token) {
		t.Fatal("expected matching authorization value to be accepted")
	}
	if Authorized(AuthorizationValue(token+"x"), token) {
		t.Fatal("expected mismatched authorization value to be rejected")
	}
}

func TestDemandAuthenticationRejectsWeakTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAuthToken(path); err == nil {
		t.Fatal("expected a weak token to be rejected")
	}
	if err := ValidateAuthToken("short"); err == nil {
		t.Fatal("expected direct validation of a weak token to fail")
	}
}
