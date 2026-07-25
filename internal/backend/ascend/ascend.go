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

// Package ascend adapts vLLM-Ascend pods to Kubernetes resources and host drivers.
package ascend

import (
	corev1 "k8s.io/api/core/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
)

const Vendor = "ascend"

// hostDriverMounts are required when the container runtime does not inject CANN drivers.
var hostDriverMounts = []backend.HostMount{
	{Name: "ascend-driver", Path: "/usr/local/Ascend/driver"},
	{Name: "npu-smi", Path: "/usr/local/bin/npu-smi"},
	{Name: "dcmi", Path: "/usr/local/dcmi"},
	{Name: "ascend-install-info", Path: "/etc/ascend_install.info"},
}

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

var _ backend.BackendAdapter = (*Adapter)(nil)

func (a *Adapter) Vendor() string { return Vendor }

func (a *Adapter) PodSpec(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime, m backend.ResolvedModel) (corev1.PodSpec, error) {
	pod, err := backend.RenderVLLMPodSpec(svc, rt, m)
	if err != nil {
		return corev1.PodSpec{}, err
	}
	backend.AddHostMounts(&pod, hostDriverMounts)
	return pod, nil
}

func (a *Adapter) Accelerator(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) (backend.AcceleratorRequest, error) {
	return backend.WholeDeviceAccelerator(svc, rt)
}
