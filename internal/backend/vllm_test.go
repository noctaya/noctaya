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

package backend_test

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
)

func TestWholeDeviceAcceleratorRejectsFraction(t *testing.T) {
	g := NewWithT(t)
	mem := resource.MustParse("12Gi")
	svc := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"},
		Spec: servingv1alpha1.LLMServiceSpec{
			Resources: servingv1alpha1.ResourceSpec{
				Fraction: &servingv1alpha1.AcceleratorFraction{Memory: &mem, Cores: 50},
			},
		},
	}
	rt := &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-nvidia"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Accelerator: servingv1alpha1.AcceleratorSpec{ResourceName: "nvidia.com/gpu"},
		},
	}
	_, err := backend.WholeDeviceAccelerator(svc, rt)
	g.Expect(err).To(MatchError(ContainSubstring("fraction")))
}

func TestRenderVLLMPodSpecCarriesImagePullSecrets(t *testing.T) {
	g := NewWithT(t)
	svc := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"},
		Spec: servingv1alpha1.LLMServiceSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "regcred"}},
		},
	}
	rt := &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-nvidia"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Container: servingv1alpha1.RuntimeContainer{
				Image: "vllm/vllm-openai:v0.22.0",
				Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
			},
		},
	}
	pod, err := backend.RenderVLLMPodSpec(svc, rt, backend.ResolvedModel{Path: "Qwen/Qwen3-8B"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: "regcred"}))
}

func TestRenderVLLMPodSpecMountsModelPVC(t *testing.T) {
	g := NewWithT(t)
	svc := &servingv1alpha1.LLMService{ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"}}
	rt := &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-nvidia"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Container: servingv1alpha1.RuntimeContainer{
				Image: "vllm/vllm-openai:v0.22.0",
				Args:  []string{"--model={{ .Model.Path }}"},
				Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
			},
		},
	}
	pod, err := backend.RenderVLLMPodSpec(svc, rt, backend.ResolvedModel{Source: "pvc", PVC: "model-store", Path: "Qwen3-8B"})
	g.Expect(err).NotTo(HaveOccurred())

	c := pod.Containers[0]
	g.Expect(c.Args).To(ContainElement("--model=/models/Qwen3-8B"))

	var mounted bool
	for _, vm := range c.VolumeMounts {
		if vm.MountPath == "/models" {
			mounted = true
			g.Expect(vm.ReadOnly).To(BeTrue())
		}
	}
	g.Expect(mounted).To(BeTrue())

	var claim string
	for _, v := range pod.Volumes {
		if v.PersistentVolumeClaim != nil {
			claim = v.PersistentVolumeClaim.ClaimName
		}
	}
	g.Expect(claim).To(Equal("model-store"))
}
