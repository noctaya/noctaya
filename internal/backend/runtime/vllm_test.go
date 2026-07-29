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

package runtime_test

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

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
	pod, err := backendruntime.RenderVLLMPodSpec(svc, rt, backendruntime.ResolvedModel{Path: "Qwen/Qwen3-8B"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: "regcred"}))
	g.Expect(pod.AutomountServiceAccountToken).To(HaveValue(BeFalse()))
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
	pod, err := backendruntime.RenderVLLMPodSpec(svc, rt, backendruntime.ResolvedModel{Source: "pvc", PVC: "model-store", Path: "Qwen3-8B"})
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

func TestRenderVLLMPodSpecWiresGracefulDrain(t *testing.T) {
	g := NewWithT(t)
	svc, runtime, model := vllmFixture()
	svc.Spec.Scaling.DrainTimeout = metav1.Duration{Duration: 90 * time.Second}
	runtime.Spec.Lifecycle.PreStopDrain = true

	pod, err := backendruntime.RenderVLLMPodSpec(svc, runtime, model)
	g.Expect(err).NotTo(HaveOccurred())
	container := vllmServingContainer(pod)
	g.Expect(container.Lifecycle).NotTo(BeNil())
	g.Expect(container.Lifecycle.PreStop.Exec.Command).To(ContainElement(ContainSubstring("sleep 90")))
	g.Expect(pod.TerminationGracePeriodSeconds).NotTo(BeNil())
	g.Expect(*pod.TerminationGracePeriodSeconds).To(BeNumerically(">=", int64(90)))
}

func TestRenderVLLMPodSpecRoundsDrainUpToWholeSeconds(t *testing.T) {
	g := NewWithT(t)
	svc, runtime, model := vllmFixture()
	svc.Spec.Scaling.DrainTimeout = metav1.Duration{Duration: 1500 * time.Millisecond}
	runtime.Spec.Lifecycle.PreStopDrain = true

	pod, err := backendruntime.RenderVLLMPodSpec(svc, runtime, model)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.Containers[0].Lifecycle.PreStop.Exec.Command).To(ContainElement(ContainSubstring("sleep 2")))
	g.Expect(pod.TerminationGracePeriodSeconds).To(HaveValue(Equal(int64(12))))
}

func TestRenderVLLMPodSpecSkipsDisabledDrain(t *testing.T) {
	g := NewWithT(t)
	svc, runtime, model := vllmFixture()
	svc.Spec.Scaling.DrainTimeout = metav1.Duration{Duration: 90 * time.Second}

	pod, err := backendruntime.RenderVLLMPodSpec(svc, runtime, model)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(vllmServingContainer(pod).Lifecycle).To(BeNil())
}

func TestRenderVLLMPodSpecDefaultsProbes(t *testing.T) {
	g := NewWithT(t)
	svc, runtime, model := vllmFixture()

	pod, err := backendruntime.RenderVLLMPodSpec(svc, runtime, model)
	g.Expect(err).NotTo(HaveOccurred())
	container := vllmServingContainer(pod)
	g.Expect(container.ReadinessProbe).NotTo(BeNil())
	g.Expect(container.StartupProbe).NotTo(BeNil())
	g.Expect(container.StartupProbe.FailureThreshold).To(BeNumerically(">=", 60))
}

func vllmFixture() (*servingv1alpha1.LLMService, *servingv1alpha1.InferenceRuntime, backendruntime.ResolvedModel) {
	svc := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"},
	}
	runtime := &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-nvidia"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Container: servingv1alpha1.RuntimeContainer{
				Image: "vllm/vllm-openai:v0.22.0",
				Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
			},
		},
	}
	model := backendruntime.ResolvedModel{Path: "Qwen/Qwen3-8B-Instruct", Source: "modelscope"}
	return svc, runtime, model
}

func vllmServingContainer(pod corev1.PodSpec) corev1.Container {
	for _, container := range pod.Containers {
		if container.Name == backendruntime.ServingContainerName {
			return container
		}
	}
	return corev1.Container{}
}
