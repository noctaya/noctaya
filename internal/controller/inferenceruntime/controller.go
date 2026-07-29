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

// Package inferenceruntime registers the passive InferenceRuntime controller.
package inferenceruntime

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

type InferenceRuntimeReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=serving.noctaya.io,resources=inferenceruntimes,verbs=get;list;watch

// Reconcile is a no-op: InferenceRuntime is a passive driver consumed by reference
// from the LLMService reconciler.
func (r *InferenceRuntimeReconciler) Reconcile(_ context.Context, _ ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *InferenceRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servingv1alpha1.InferenceRuntime{}).
		Named("inferenceruntime").
		Complete(r)
}
