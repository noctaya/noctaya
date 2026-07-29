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
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

func TestServiceForBackendPod(t *testing.T) {
	reconciler := &LLMServiceReconciler{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      "model-abc",
		Namespace: "models",
		Labels: map[string]string{
			managedByLabel:  managedByValue,
			llmServiceLabel: "model",
			runtimeLabel:    "vllm",
		},
	}}
	requests := reconciler.serviceForBackendPod(context.Background(), pod)
	if len(requests) != 1 ||
		requests[0].NamespacedName != (types.NamespacedName{Name: "model", Namespace: "models"}) {
		t.Fatalf("unexpected requests: %#v", requests)
	}

	delete(pod.Labels, runtimeLabel)
	if requests := reconciler.serviceForBackendPod(context.Background(), pod); len(requests) != 0 {
		t.Fatalf("gateway or prewarm Pod produced requests: %#v", requests)
	}
}

func TestServicesForRuntime(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(servingv1alpha1.AddToScheme(scheme)).To(Succeed())

	services := []servingv1alpha1.LLMService{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pinned", Namespace: "ai"},
			Spec: servingv1alpha1.LLMServiceSpec{
				Runtime: servingv1alpha1.RuntimeSelection{Name: "vllm-ascend"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "selected", Namespace: "models"},
			Spec: servingv1alpha1.LLMServiceSpec{Runtime: servingv1alpha1.RuntimeSelection{
				Selector: &servingv1alpha1.RuntimeSelector{Vendor: []string{"ascend", "nvidia"}},
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "ai"},
			Spec: servingv1alpha1.LLMServiceSpec{
				Runtime: servingv1alpha1.RuntimeSelection{Name: "vllm-nvidia"},
			},
		},
	}
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithLists(&servingv1alpha1.LLMServiceList{Items: services}).
		Build()
	reconciler := &LLMServiceReconciler{Client: client}
	runtimeProfile := &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-ascend"},
		Spec:       servingv1alpha1.InferenceRuntimeSpec{Vendor: "ascend"},
	}

	g.Expect(reconciler.servicesForRuntime(context.Background(), runtimeProfile)).To(ConsistOf(
		reconcile.Request{NamespacedName: types.NamespacedName{Name: "pinned", Namespace: "ai"}},
		reconcile.Request{NamespacedName: types.NamespacedName{Name: "selected", Namespace: "models"}},
	))
}
