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

package controller

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

func rt(name, vendor string, priority int32) servingv1alpha1.InferenceRuntime {
	return servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       servingv1alpha1.InferenceRuntimeSpec{Vendor: vendor, Priority: priority},
	}
}

func TestPickByVendorPreferenceOrderBeatsPriority(t *testing.T) {
	g := NewWithT(t)
	items := []servingv1alpha1.InferenceRuntime{
		rt("vllm-nvidia", "nvidia", 100),
		rt("vllm-ascend", "ascend", 10),
	}
	got, err := pickByVendor(items, []string{"ascend", "nvidia"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.Name).To(Equal("vllm-ascend"))
}

func TestPickByVendorPriorityTieBreak(t *testing.T) {
	g := NewWithT(t)
	items := []servingv1alpha1.InferenceRuntime{
		rt("vllm-ascend-alt", "ascend", 90),
		rt("vllm-ascend", "ascend", 100),
	}
	got, err := pickByVendor(items, []string{"ascend"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.Name).To(Equal("vllm-ascend"))
}

func TestPickByVendorRejectsAmbiguousPriority(t *testing.T) {
	g := NewWithT(t)
	items := []servingv1alpha1.InferenceRuntime{
		rt("vllm-ascend-310p-pro", "ascend", 90),
		rt("vllm-ascend-310p-duo", "ascend", 90),
	}

	_, err := pickByVendor(items, []string{"ascend"})
	g.Expect(err).To(MatchError(And(
		ContainSubstring("multiple InferenceRuntimes"),
		ContainSubstring("set spec.runtime.name"),
	)))
}

func TestPickByVendorFallsThroughToNextVendor(t *testing.T) {
	g := NewWithT(t)
	items := []servingv1alpha1.InferenceRuntime{
		rt("vllm-nvidia", "nvidia", 0),
	}
	got, err := pickByVendor(items, []string{"unsupported", "nvidia"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.Name).To(Equal("vllm-nvidia"))
}

func TestPickByVendorNoMatch(t *testing.T) {
	g := NewWithT(t)
	items := []servingv1alpha1.InferenceRuntime{
		rt("vllm-nvidia", "nvidia", 0),
	}
	_, err := pickByVendor(items, []string{"ascend"})
	g.Expect(err).To(MatchError(ContainSubstring("ascend")))
}

func TestServicesForRuntime(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(servingv1alpha1.AddToScheme(scheme)).To(Succeed())

	services := []servingv1alpha1.LLMService{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pinned", Namespace: "ai"},
			Spec:       servingv1alpha1.LLMServiceSpec{Runtime: servingv1alpha1.RuntimeSelection{Name: "vllm-ascend"}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "selected", Namespace: "models"},
			Spec: servingv1alpha1.LLMServiceSpec{Runtime: servingv1alpha1.RuntimeSelection{
				Selector: &servingv1alpha1.RuntimeSelector{Vendor: []string{"ascend", "nvidia"}},
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "ai"},
			Spec:       servingv1alpha1.LLMServiceSpec{Runtime: servingv1alpha1.RuntimeSelection{Name: "vllm-nvidia"}},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithLists(&servingv1alpha1.LLMServiceList{Items: services}).Build()
	r := &LLMServiceReconciler{Client: client}
	rt := &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-ascend"},
		Spec:       servingv1alpha1.InferenceRuntimeSpec{Vendor: "ascend"},
	}

	g.Expect(r.servicesForRuntime(context.Background(), rt)).To(ConsistOf(
		reconcile.Request{NamespacedName: types.NamespacedName{Name: "pinned", Namespace: "ai"}},
		reconcile.Request{NamespacedName: types.NamespacedName{Name: "selected", Namespace: "models"}},
	))
}
