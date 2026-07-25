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

// Package registry wires the built-in backend adapters into a Registry. It lives in
// its own package so adapter packages can import backend without an import cycle.
package registry

import (
	"github.com/noctaya/noctaya/internal/backend"
	"github.com/noctaya/noctaya/internal/backend/ascend"
	"github.com/noctaya/noctaya/internal/backend/nvidia"
)

// New returns the registry of built-in Kubernetes adapters.
func New() *backend.Registry {
	r := backend.NewRegistry()
	r.Register(nvidia.New())
	r.Register(ascend.New())
	return r
}
