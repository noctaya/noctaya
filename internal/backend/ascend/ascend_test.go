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

package ascend_test

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend/ascend"
	"github.com/noctaya/noctaya/internal/backend/resources"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

func ascendRuntime() *servingv1alpha1.InferenceRuntime {
	return &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-ascend"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Family: "vllm",
			Vendor: "ascend",
			Container: servingv1alpha1.RuntimeContainer{
				Image: "quay.io/ascend/vllm-ascend:v0.21.0rc1",
				Args:  []string{"vllm", "serve", "{{ .Model.Path }}", "--served-model-name={{ .Service.Name }}"},
				Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
			},
			Accelerator: servingv1alpha1.AcceleratorSpec{
				ResourceName: "huawei.com/Ascend910",
				NodeSelector: map[string]string{"accelerator": "huawei-Ascend910"},
			},
		},
	}
}

func ascendService() *servingv1alpha1.LLMService {
	return &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "deepseek-r1", Namespace: "ai"},
		Spec: servingv1alpha1.LLMServiceSpec{
			Model:   servingv1alpha1.ModelSpec{Source: &servingv1alpha1.ModelSource{URI: "modelscope://deepseek-ai/DeepSeek-R1-Distill-Qwen-7B"}},
			Runtime: servingv1alpha1.RuntimeSelection{Name: "vllm-ascend"},
		},
	}
}

func resolved() backendruntime.ResolvedModel {
	return backendruntime.ResolvedModel{Path: "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B"}
}

func TestSameFrameworkRendersAscend(t *testing.T) {
	g := NewWithT(t)
	dep, err := resources.BuildBackendDeployment(ascend.New(), ascendService(), ascendRuntime(), resolved())
	g.Expect(err).NotTo(HaveOccurred())

	c := dep.Spec.Template.Spec.Containers[0]
	g.Expect(c.Image).To(Equal("quay.io/ascend/vllm-ascend:v0.21.0rc1"))
	g.Expect(c.Args[:3]).To(Equal([]string{"vllm", "serve", "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B"}))

	g.Expect(c.Resources.Limits).To(HaveKey(corev1.ResourceName("huawei.com/Ascend910")))
	g.Expect(dep.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("accelerator", "huawei-Ascend910"))
}

func TestAscendProjectsDriverMounts(t *testing.T) {
	g := NewWithT(t)
	pod, err := ascend.New().PodSpec(ascendService(), ascendRuntime(), resolved())
	g.Expect(err).NotTo(HaveOccurred())

	volNames := map[string]bool{}
	for _, v := range pod.Volumes {
		volNames[v.Name] = true
	}
	g.Expect(volNames).To(HaveKey("dshm"))
	g.Expect(volNames).To(HaveKey("npu-smi"))
	g.Expect(volNames).To(HaveKey("dcmi"))
	g.Expect(volNames).To(HaveKey("ascend-driver"))

	c := pod.Containers[0]
	var npuSmi *corev1.VolumeMount
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == "npu-smi" {
			npuSmi = &c.VolumeMounts[i]
		}
	}
	g.Expect(npuSmi).NotTo(BeNil())
	g.Expect(npuSmi.ReadOnly).To(BeTrue())
}

func TestVendor(t *testing.T) {
	g := NewWithT(t)
	g.Expect(ascend.New().Vendor()).To(Equal("ascend"))
}
