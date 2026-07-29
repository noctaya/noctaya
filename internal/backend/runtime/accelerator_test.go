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

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

func TestWholeDeviceAcceleratorRejectsFraction(t *testing.T) {
	g := NewWithT(t)
	memory := resource.MustParse("12Gi")
	svc := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"},
		Spec: servingv1alpha1.LLMServiceSpec{
			Resources: servingv1alpha1.ResourceSpec{
				Fraction: &servingv1alpha1.AcceleratorFraction{Memory: &memory, Cores: 50},
			},
		},
	}
	runtime := &servingv1alpha1.InferenceRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "vllm-nvidia"},
		Spec: servingv1alpha1.InferenceRuntimeSpec{
			Accelerator: servingv1alpha1.AcceleratorSpec{ResourceName: "nvidia.com/gpu"},
		},
	}
	_, err := backendruntime.WholeDeviceAccelerator(svc, runtime)
	g.Expect(err).To(MatchError(ContainSubstring("fraction")))
}
