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

// Package runtime defines vendor-neutral rendering for inference runtimes.
package runtime

import (
	corev1 "k8s.io/api/core/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

type ResolvedModel struct {
	Path string
	// Source ("hf" | "modelscope" | "pvc") selects the prewarm download command, or
	// "pvc" for weights pre-staged on a PersistentVolumeClaim (no download).
	Source string
	Env    []corev1.EnvVar
	// PVC, when set (pvc:// source), is an existing claim mounted read-only at the model
	// mount path; Path is then the model's subpath within that PVC. No prewarm is run.
	PVC string
}

type AcceleratorRequest struct {
	Resources     corev1.ResourceList
	NodeSelector  map[string]string
	Tolerations   []corev1.Toleration
	SchedulerName string
	// Queue is the Volcano queue rendered on the backend Deployment.
	Queue string
}

// BackendAdapter renders the K8s artifacts for one vendor runtime.
type BackendAdapter interface {
	Vendor() string
	PodSpec(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime, m ResolvedModel) (corev1.PodSpec, error)
	Accelerator(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) (AcceleratorRequest, error)
}
