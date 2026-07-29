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

// Package registry owns adapter registration and lookup.
package registry

import (
	"github.com/noctaya/noctaya/internal/backend/ascend"
	"github.com/noctaya/noctaya/internal/backend/nvidia"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

type Registry struct {
	adapters map[string]backendruntime.BackendAdapter
}

func New() *Registry {
	registry := &Registry{adapters: map[string]backendruntime.BackendAdapter{}}
	registry.Register(nvidia.New())
	registry.Register(ascend.New())
	return registry
}

func (r *Registry) Register(adapter backendruntime.BackendAdapter) {
	r.adapters[adapter.Vendor()] = adapter
}

func (r *Registry) Get(vendor string) (backendruntime.BackendAdapter, bool) {
	adapter, ok := r.adapters[vendor]
	return adapter, ok
}
