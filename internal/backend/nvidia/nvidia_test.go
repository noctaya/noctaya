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

package nvidia_test

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
	"github.com/noctaya/noctaya/internal/backend/nvidia"
)

func sampleRuntime() *servingv1alpha1.InferenceRuntime {
	return &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-nvidia"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Family: "vllm",
			Vendor: "nvidia",
			Container: servingv1alpha1.RuntimeContainer{
				Image: "vllm/vllm-openai:v0.22.0",
				Args:  []string{"--model={{ .Model.Path }}", "--served-model-name={{ .Service.Name }}", "--port=8000"},
				Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
			},
			Accelerator: servingv1alpha1.AcceleratorSpec{
				ResourceName: "nvidia.com/gpu",
				NodeSelector: map[string]string{"accelerator": "nvidia"},
			},
			Health: servingv1alpha1.RuntimeHealth{
				Readiness: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString("http")}}},
			},
		},
	}
}

func sampleService() *servingv1alpha1.LLMService {
	cpu := resource.MustParse("8")
	mem := resource.MustParse("32Gi")
	return &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"},
		Spec: servingv1alpha1.LLMServiceSpec{
			Model: servingv1alpha1.ModelSpec{Source: &servingv1alpha1.ModelSource{URI: "modelscope://Qwen/Qwen3-8B-Instruct"}},
			Runtime: servingv1alpha1.RuntimeSelection{
				Name:         "vllm-nvidia",
				ArgsOverride: []string{"--max-model-len=8192"},
			},
			Resources: servingv1alpha1.ResourceSpec{Accelerators: 1, CPU: &cpu, Memory: &mem},
		},
	}
}

func resolvedModel() backend.ResolvedModel {
	return backend.ResolvedModel{
		Path: "Qwen/Qwen3-8B-Instruct",
		Env:  []corev1.EnvVar{{Name: "VLLM_USE_MODELSCOPE", Value: "true"}},
	}
}

func TestPodSpecRendersContainer(t *testing.T) {
	g := NewWithT(t)
	pod, err := nvidia.New().PodSpec(sampleService(), sampleRuntime(), resolvedModel())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.Containers).To(HaveLen(1))

	c := pod.Containers[0]
	g.Expect(c.Name).To(Equal(backend.ServingContainerName))
	g.Expect(c.Image).To(Equal("vllm/vllm-openai:v0.22.0"))
	g.Expect(c.Args).To(Equal([]string{
		"--model=Qwen/Qwen3-8B-Instruct",
		"--served-model-name=qwen3-8b",
		"--port=8000",
		"--max-model-len=8192",
	}))
	g.Expect(c.Env).To(ContainElement(corev1.EnvVar{Name: "VLLM_USE_MODELSCOPE", Value: "true"}))
	g.Expect(c.ReadinessProbe).NotTo(BeNil())
	g.Expect(c.Resources.Requests).To(HaveKey(corev1.ResourceCPU))
	g.Expect(c.Resources.Limits).To(HaveKey(corev1.ResourceMemory))

	g.Expect(c.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: "dshm", MountPath: "/dev/shm"}))
	g.Expect(pod.Volumes).To(HaveLen(1))
	g.Expect(pod.Volumes[0].EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory))
}

func TestAcceleratorMapsResource(t *testing.T) {
	g := NewWithT(t)
	accel, err := nvidia.New().Accelerator(sampleService(), sampleRuntime())
	g.Expect(err).NotTo(HaveOccurred())

	q := accel.Resources[corev1.ResourceName("nvidia.com/gpu")]
	g.Expect(q.Value()).To(Equal(int64(1)))
	g.Expect(accel.NodeSelector).To(HaveKeyWithValue("accelerator", "nvidia"))
}

func TestBuildDeploymentAssembles(t *testing.T) {
	g := NewWithT(t)
	dep, err := backend.BuildDeployment(nvidia.New(), sampleService(), sampleRuntime(), resolvedModel())
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(dep.Spec.Replicas).To(BeNil())
	g.Expect(dep.Spec.Selector.MatchLabels).To(HaveKeyWithValue("serving.noctaya.io/llmservice", "qwen3-8b"))
	g.Expect(dep.Spec.Template.Labels).To(HaveKeyWithValue("serving.noctaya.io/runtime", "vllm-nvidia"))

	c := dep.Spec.Template.Spec.Containers[0]
	g.Expect(c.Resources.Limits).To(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
}

func TestBuildDeploymentRendersVolcanoQueue(t *testing.T) {
	g := NewWithT(t)
	rt := sampleRuntime()
	rt.Spec.Accelerator.Scheduler = servingv1alpha1.RuntimeScheduler{Name: "volcano", Queue: "inference"}

	dep, err := backend.BuildDeployment(nvidia.New(), sampleService(), rt, resolvedModel())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dep.Spec.Template.Spec.SchedulerName).To(Equal("volcano"))
	g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue("scheduling.volcano.sh/queue-name", "inference"))

	plain, err := backend.BuildDeployment(nvidia.New(), sampleService(), sampleRuntime(), resolvedModel())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plain.Spec.Template.Annotations).NotTo(HaveKey("scheduling.volcano.sh/queue-name"))
}

func TestBuildDeploymentRejectsQueueWithoutVolcano(t *testing.T) {
	g := NewWithT(t)
	rt := sampleRuntime()
	rt.Spec.Accelerator.Scheduler.Queue = "inference"

	_, err := backend.BuildDeployment(nvidia.New(), sampleService(), rt, resolvedModel())
	g.Expect(err).To(MatchError(ContainSubstring("scheduler.name=volcano")))
}

func TestPodSpecRejectsBadTemplate(t *testing.T) {
	g := NewWithT(t)
	rt := sampleRuntime()
	rt.Spec.Container.Args = []string{"--model={{ .Nope }}"}
	_, err := nvidia.New().PodSpec(sampleService(), rt, resolvedModel())
	g.Expect(err).To(HaveOccurred())
}
