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
	"testing"

	"k8s.io/apimachinery/pkg/labels"
)

func TestManagerCachesOnlyNoctayaManagedSecrets(t *testing.T) {
	options := managerCacheOptions()
	if len(options.ByObject) != 1 {
		t.Fatalf("secret cache entries = %d, want 1", len(options.ByObject))
	}
	for _, byObject := range options.ByObject {
		if byObject.Label.Matches(labels.Set{"app": "client-secret"}) {
			t.Fatal("unmanaged client Secret matched the controller cache")
		}
		if !byObject.Label.Matches(labels.Set{"app.kubernetes.io/managed-by": "noctaya"}) {
			t.Fatal("Noctaya-managed Secret did not match the controller cache")
		}
	}
}
