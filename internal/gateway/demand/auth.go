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
	"crypto/subtle"
	"fmt"
	"os"
	"strings"
)

const (
	EnvAuthTokenFile = "NOCTAYA_DEMAND_AUTH_TOKEN_FILE"
	authScheme       = "Bearer "
)

func ReadAuthToken(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("demand authentication token file is required")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read demand authentication token: %w", err)
	}
	token := strings.TrimSpace(string(value))
	if err := ValidateAuthToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func ValidateAuthToken(token string) error {
	token = strings.TrimSpace(token)
	if len(token) < 32 {
		return fmt.Errorf("demand authentication token must contain at least 32 characters")
	}
	return nil
}

func AuthorizationValue(token string) string {
	return authScheme + token
}

func Authorized(value, token string) bool {
	expected := AuthorizationValue(token)
	return len(value) == len(expected) &&
		subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}
