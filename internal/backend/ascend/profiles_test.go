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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
	"github.com/noctaya/noctaya/internal/backend/ascend"
	"github.com/noctaya/noctaya/internal/model"
)

const examplesDir = "../../../examples/ascend"

func loadExample[T any](t *testing.T, name string) *T {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(examplesDir, name)) //nolint:gosec // test reads a fixed repository fixture
	if err != nil {
		t.Fatalf("read example %s: %v", name, err)
	}
	jsonData, err := utilyaml.ToJSON(data)
	if err != nil {
		t.Fatalf("convert example %s to JSON: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(jsonData, &out); err != nil {
		t.Fatalf("decode example %s: %v", name, err)
	}
	return &out
}

func TestAscend910B3Profile(t *testing.T) {
	g := NewWithT(t)
	rt := loadExample[servingv1alpha1.InferenceRuntime](t, "910b3/serving_v1alpha1_inferenceruntime.yaml")
	svc := loadExample[servingv1alpha1.LLMService](t, "910b3/serving_v1alpha1_llmservice.yaml")

	g.Expect(rt.Name).To(Equal("vllm-ascend"))
	g.Expect(rt.Spec.Vendor).To(Equal(ascend.Vendor))
	g.Expect(rt.Spec.Container.Image).To(Equal("quay.io/ascend/vllm-ascend:v0.21.0rc1"))
	g.Expect(rt.Spec.Container.Args[:3]).To(Equal([]string{"vllm", "serve", "{{ .Model.Path }}"}))
	g.Expect(rt.Spec.Accelerator.ResourceName).To(Equal("huawei.com/Ascend910"))
	g.Expect(rt.Spec.Accelerator.NodeSelector).To(HaveKeyWithValue("accelerator", "huawei-Ascend910"))
	g.Expect(rt.Spec.Accelerator.NodeSelector).To(HaveKeyWithValue("serving.noctaya.io/ascend-product", "ascend-910b3"))
	g.Expect(svc.Spec.Runtime.Name).To(Equal(rt.Name))
	g.Expect(svc.Spec.Scaling.Max).To(Equal(int32(1)))
	g.Expect(svc.Spec.Scaling.DrainTimeout.Duration.String()).To(Equal("1m0s"))
	g.Expect(svc.Spec.Cache.Prewarm).To(BeTrue())

	resolved, err := model.Resolve(svc.Spec.Model)
	g.Expect(err).NotTo(HaveOccurred())
	deployment, err := backend.BuildDeployment(ascend.New(), svc, rt, resolved)
	g.Expect(err).NotTo(HaveOccurred())

	container := deployment.Spec.Template.Spec.Containers[0]
	g.Expect(container.Args[:3]).To(Equal([]string{"vllm", "serve", "Qwen/Qwen2.5-0.5B-Instruct"}))
	g.Expect(container.Resources.Limits).To(HaveKey(corev1.ResourceName("huawei.com/Ascend910")))
	g.Expect(deployment.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "ascend-driver")))

	prewarm, err := backend.BuildPrewarmJob(svc, rt, resolved)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(prewarm).NotTo(BeNil())
	g.Expect(prewarm.Spec.Template.Spec.Containers[0].Resources.Limits).NotTo(HaveKey(corev1.ResourceName("huawei.com/Ascend910")))
	g.Expect(prewarm.Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{
		Name: "TORCH_DEVICE_BACKEND_AUTOLOAD", Value: "0",
	}))
}

func TestAscend310PProfiles(t *testing.T) {
	profiles := []struct {
		name        string
		product     string
		runtimeName string
		runtimeFile string
		serviceFile string
		maxReplicas int32
	}{
		{
			name:        "Atlas 300I Duo",
			product:     "atlas-300i-duo",
			runtimeName: "vllm-ascend-310p-duo",
			runtimeFile: "310p-duo/serving_v1alpha1_inferenceruntime.yaml",
			serviceFile: "310p-duo/serving_v1alpha1_llmservice.yaml",
			maxReplicas: 2,
		},
		{
			name:        "Atlas 300I Pro",
			product:     "atlas-300i-pro",
			runtimeName: "vllm-ascend-310p-pro",
			runtimeFile: "310p-pro/serving_v1alpha1_inferenceruntime.yaml",
			serviceFile: "310p-pro/serving_v1alpha1_llmservice.yaml",
			maxReplicas: 1,
		},
	}

	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			g := NewWithT(t)
			rt := loadExample[servingv1alpha1.InferenceRuntime](t, profile.runtimeFile)
			svc := loadExample[servingv1alpha1.LLMService](t, profile.serviceFile)

			g.Expect(rt.Name).To(Equal(profile.runtimeName))
			g.Expect(rt.Spec.Vendor).To(Equal(ascend.Vendor))
			g.Expect(rt.Spec.Container.Image).To(Equal("quay.io/ascend/vllm-ascend:v0.22.1rc1-310p"))
			g.Expect(rt.Spec.Accelerator.ResourceName).To(Equal("huawei.com/Ascend310P"))
			g.Expect(rt.Spec.Accelerator.NodeSelector).To(HaveKeyWithValue("accelerator", "huawei-Ascend310P"))
			g.Expect(rt.Spec.Accelerator.NodeSelector).To(HaveKeyWithValue("serving.noctaya.io/ascend-product", profile.product))
			g.Expect(rt.Spec.Container.Args[:3]).To(Equal([]string{"vllm", "serve", "{{ .Model.Path }}"}))
			g.Expect(svc.Spec.Runtime.Name).To(Equal(profile.runtimeName))
			g.Expect(svc.Spec.Runtime.ArgsOverride).To(ContainElements("--dtype=float16", "--enforce-eager", "--max-model-len=2048"))
			g.Expect(svc.Spec.Scaling.Max).To(Equal(profile.maxReplicas))

			resolved, err := model.Resolve(svc.Spec.Model)
			g.Expect(err).NotTo(HaveOccurred())
			deployment, err := backend.BuildDeployment(ascend.New(), svc, rt, resolved)
			g.Expect(err).NotTo(HaveOccurred())

			container := deployment.Spec.Template.Spec.Containers[0]
			g.Expect(container.Args[:3]).To(Equal([]string{"vllm", "serve", "Qwen/Qwen2.5-0.5B-Instruct"}))
			g.Expect(container.Resources.Limits).To(HaveKey(corev1.ResourceName("huawei.com/Ascend310P")))
			g.Expect(deployment.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "ascend-driver")))
		})
	}
}
