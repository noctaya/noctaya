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

package resources_test

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend/resources"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

type stubAdapter struct{}

func (stubAdapter) Vendor() string { return "stub" }
func (stubAdapter) PodSpec(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime, m backendruntime.ResolvedModel) (corev1.PodSpec, error) {
	return backendruntime.RenderVLLMPodSpec(svc, rt, m)
}
func (stubAdapter) Accelerator(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) (backendruntime.AcceleratorRequest, error) {
	return backendruntime.WholeDeviceAccelerator(svc, rt)
}

func runtimeFixture() *servingv1alpha1.InferenceRuntime {
	return &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "rt"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Vendor: "stub",
			Container: servingv1alpha1.RuntimeContainer{
				Image: "vllm/vllm-openai:v0.22.0",
				Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
			},
			Accelerator: servingv1alpha1.AcceleratorSpec{ResourceName: "nvidia.com/gpu"},
		},
	}
}

func serviceWithCache(strategy string, prewarm bool) *servingv1alpha1.LLMService {
	return &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"},
		Spec: servingv1alpha1.LLMServiceSpec{
			Model:   servingv1alpha1.ModelSpec{Source: &servingv1alpha1.ModelSource{URI: "modelscope://Qwen/Qwen3-8B-Instruct"}},
			Runtime: servingv1alpha1.RuntimeSelection{Name: "rt"},
			Cache:   servingv1alpha1.CacheSpec{Strategy: strategy, Prewarm: prewarm},
		},
	}
}

func model() backendruntime.ResolvedModel {
	return backendruntime.ResolvedModel{Path: "Qwen/Qwen3-8B-Instruct", Source: "modelscope"}
}

func servingContainer(pod corev1.PodSpec) corev1.Container {
	for _, c := range pod.Containers {
		if c.Name == backendruntime.ServingContainerName {
			return c
		}
	}
	return corev1.Container{}
}

func TestPrewarmJobCarriesImagePullSecrets(t *testing.T) {
	g := NewWithT(t)
	svc := serviceWithCache("NodeLocalPVC", true)
	svc.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "regcred"}}
	job, err := resources.BuildPrewarmJob(svc, runtimeFixture(), model())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job.Spec.Template.Spec.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: "regcred"}))
	g.Expect(job.Spec.Template.Spec.AutomountServiceAccountToken).To(HaveValue(BeFalse()))
}

func TestPrewarmJobSkippedForPVCSource(t *testing.T) {
	g := NewWithT(t)
	svc := serviceWithCache("NodeLocalPVC", true)
	job, err := resources.BuildPrewarmJob(svc, runtimeFixture(), backendruntime.ResolvedModel{Source: "pvc", PVC: "model-store", Path: "m"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job).To(BeNil()) // weights are pre-staged on the PVC; nothing to download
}

func TestNodeLocalPVCCache(t *testing.T) {
	g := NewWithT(t)
	svc := serviceWithCache("NodeLocalPVC", false)

	dep, err := resources.BuildBackendDeployment(stubAdapter{}, svc, runtimeFixture(), model())
	g.Expect(err).NotTo(HaveOccurred())
	c := servingContainer(dep.Spec.Template.Spec)
	g.Expect(c.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: "model-cache", MountPath: "/cache"}))
	g.Expect(c.Env).To(ContainElement(corev1.EnvVar{Name: "HF_HOME", Value: "/cache/hf"}))

	pvc, err := resources.BuildCachePVC(svc)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pvc).NotTo(BeNil())
	g.Expect(pvc.Name).To(Equal("qwen3-8b-cache"))
	g.Expect(pvc.Spec.AccessModes).To(ContainElement(corev1.ReadWriteOnce))
	g.Expect(pvc.Spec.Resources.Requests).To(HaveKey(corev1.ResourceStorage))
}

func TestHostPathCacheHasNoPVC(t *testing.T) {
	g := NewWithT(t)
	svc := serviceWithCache("HostPath", false)

	dep, err := resources.BuildBackendDeployment(stubAdapter{}, svc, runtimeFixture(), model())
	g.Expect(err).NotTo(HaveOccurred())
	var cacheVol *corev1.Volume
	for i := range dep.Spec.Template.Spec.Volumes {
		if dep.Spec.Template.Spec.Volumes[i].Name == "model-cache" {
			cacheVol = &dep.Spec.Template.Spec.Volumes[i]
		}
	}
	g.Expect(cacheVol).NotTo(BeNil())
	g.Expect(cacheVol.HostPath).NotTo(BeNil())

	pvc, err := resources.BuildCachePVC(svc)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pvc).To(BeNil())
}

func TestNoneCacheMountsNothing(t *testing.T) {
	g := NewWithT(t)
	svc := serviceWithCache("None", false)

	dep, err := resources.BuildBackendDeployment(stubAdapter{}, svc, runtimeFixture(), model())
	g.Expect(err).NotTo(HaveOccurred())
	for _, v := range dep.Spec.Template.Spec.Volumes {
		g.Expect(v.Name).NotTo(Equal("model-cache"))
	}
}

func TestSharedPVCNotSupportedYet(t *testing.T) {
	g := NewWithT(t)
	_, err := resources.BuildBackendDeployment(stubAdapter{}, serviceWithCache("SharedPVC", false), runtimeFixture(), model())
	g.Expect(err).To(HaveOccurred())
}

func TestBakedImageNotSupportedYet(t *testing.T) {
	g := NewWithT(t)
	_, err := resources.BuildBackendDeployment(stubAdapter{}, serviceWithCache("BakedImage", false), runtimeFixture(), model())
	g.Expect(err).To(MatchError(ContainSubstring("BakedImage")))
}

func TestPrewarmJob(t *testing.T) {
	g := NewWithT(t)

	job, err := resources.BuildPrewarmJob(serviceWithCache("NodeLocalPVC", true), runtimeFixture(), model())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job).NotTo(BeNil())
	jc := job.Spec.Template.Spec.Containers[0]
	g.Expect(jc.Image).To(Equal("vllm/vllm-openai:v0.22.0"))
	g.Expect(jc.Command).To(ContainElement(ContainSubstring("modelscope")))
	g.Expect(jc.Env).To(ContainElement(corev1.EnvVar{Name: "TORCH_DEVICE_BACKEND_AUTOLOAD", Value: "0"}))
	g.Expect(jc.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: "model-cache", MountPath: "/cache"}))

	job, err = resources.BuildPrewarmJob(serviceWithCache("NodeLocalPVC", false), runtimeFixture(), model())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job).To(BeNil())

	job, err = resources.BuildPrewarmJob(serviceWithCache("None", true), runtimeFixture(), model())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job).To(BeNil())
}

func TestPrewarmJobUsesRuntimeScheduling(t *testing.T) {
	g := NewWithT(t)
	rt := runtimeFixture()
	rt.Spec.Accelerator.NodeSelector = map[string]string{"accelerator": "ascend-310p"}
	rt.Spec.Accelerator.Tolerations = []corev1.Toleration{{Key: "npu", Operator: corev1.TolerationOpExists}}
	rt.Spec.Accelerator.Scheduler = servingv1alpha1.RuntimeScheduler{Name: "volcano", Queue: "inference"}

	job, err := resources.BuildPrewarmJob(serviceWithCache("HostPath", true), rt, model())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("accelerator", "ascend-310p"))
	g.Expect(job.Spec.Template.Spec.Tolerations).To(HaveLen(1))
	g.Expect(job.Spec.Template.Spec.SchedulerName).To(Equal("volcano"))
	g.Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue("scheduling.volcano.sh/queue-name", "inference"))
	g.Expect(job.Spec.Template.Spec.Containers[0].Resources.Limits).NotTo(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
}
