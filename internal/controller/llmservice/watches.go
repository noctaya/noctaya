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

package llmservice

import (
	"context"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

func (r *LLMServiceReconciler) serviceForBackendPod(_ context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok ||
		pod.Labels[managedByLabel] != managedByValue ||
		pod.Labels[runtimeLabel] == "" ||
		pod.Labels[llmServiceLabel] == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: client.ObjectKey{
			Name:      pod.Labels[llmServiceLabel],
			Namespace: pod.Namespace,
		},
	}}
}

func (r *LLMServiceReconciler) servicesForRuntime(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	runtime, ok := obj.(*servingv1alpha1.InferenceRuntime)
	if !ok {
		return nil
	}

	var services servingv1alpha1.LLMServiceList
	if err := r.List(ctx, &services); err != nil {
		logf.FromContext(ctx).Error(
			err,
			"Failed to list LLMServices for InferenceRuntime",
			"runtime",
			runtime.Name,
		)
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for i := range services.Items {
		service := &services.Items[i]
		selection := service.Spec.Runtime
		if selection.Name == runtime.Name ||
			(selection.Selector != nil && slices.Contains(selection.Selector.Vendor, runtime.Spec.Vendor)) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(service)})
		}
	}
	return requests
}
