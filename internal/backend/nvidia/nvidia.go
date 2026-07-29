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

// Package nvidia is the v0 backend adapter for NVIDIA GPUs running NVIDIA-vLLM.
// Almost everything is declarative in the InferenceRuntime, so the adapter is thin:
// it serves whole GPUs via the standard device plugin. Fractional GPU (HAMi) lands later.
package nvidia

import (
	corev1 "k8s.io/api/core/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

const Vendor = "nvidia"

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

var _ backendruntime.BackendAdapter = (*Adapter)(nil)

func (a *Adapter) Vendor() string { return Vendor }

func (a *Adapter) PodSpec(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime, m backendruntime.ResolvedModel) (corev1.PodSpec, error) {
	return backendruntime.RenderVLLMPodSpec(svc, rt, m)
}

func (a *Adapter) Accelerator(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) (backendruntime.AcceleratorRequest, error) {
	return backendruntime.WholeDeviceAccelerator(svc, rt)
}
