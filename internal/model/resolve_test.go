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

package model_test

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/model"
)

func src(uri string) servingv1alpha1.ModelSpec {
	return servingv1alpha1.ModelSpec{Source: &servingv1alpha1.ModelSource{URI: uri}}
}

func TestResolveModelScope(t *testing.T) {
	g := NewWithT(t)
	got, err := model.Resolve(src("modelscope://Qwen/Qwen3-8B-Instruct"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.Path).To(Equal("Qwen/Qwen3-8B-Instruct"))
	g.Expect(got.Env).To(ContainElement(corev1.EnvVar{Name: "VLLM_USE_MODELSCOPE", Value: "true"}))
}

func TestResolveHuggingFace(t *testing.T) {
	g := NewWithT(t)
	got, err := model.Resolve(src("hf://meta-llama/Llama-3-8B"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.Path).To(Equal("meta-llama/Llama-3-8B"))
	g.Expect(got.Env).To(BeEmpty())
}

func TestResolvePVC(t *testing.T) {
	g := NewWithT(t)
	got, err := model.Resolve(src("pvc://model-store/Qwen3-8B-Instruct"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.Source).To(Equal("pvc"))
	g.Expect(got.PVC).To(Equal("model-store"))
	g.Expect(got.Path).To(Equal("Qwen3-8B-Instruct"))
	g.Expect(got.Env).To(BeEmpty())
}

func TestResolvePVCWholeVolume(t *testing.T) {
	g := NewWithT(t)
	got, err := model.Resolve(src("pvc://model-store"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.PVC).To(Equal("model-store"))
	g.Expect(got.Path).To(Equal(""))
}

func TestResolveErrors(t *testing.T) {
	g := NewWithT(t)

	_, err := model.Resolve(servingv1alpha1.ModelSpec{})
	g.Expect(err).To(HaveOccurred())

	_, err = model.Resolve(src("no-scheme"))
	g.Expect(err).To(HaveOccurred())

	_, err = model.Resolve(src("s3://bucket/model"))
	g.Expect(err).To(HaveOccurred())

	_, err = model.Resolve(src("pvc:///model")) // empty PVC name
	g.Expect(err).To(HaveOccurred())

	_, err = model.Resolve(src("pvc://model-store/../secret"))
	g.Expect(err).To(MatchError(ContainSubstring("subpath")))

	_, err = model.Resolve(src("pvc://Invalid_Claim/model"))
	g.Expect(err).To(MatchError(ContainSubstring("pvc uri")))

	_, err = model.Resolve(servingv1alpha1.ModelSpec{CatalogRef: "qwen3-8b-instruct"})
	g.Expect(err).To(HaveOccurred())
}

func TestResolveRejectsSecretRef(t *testing.T) {
	g := NewWithT(t)
	spec := src("modelscope://Qwen/Qwen3-8B-Instruct")
	spec.Source.SecretRef = &corev1.LocalObjectReference{Name: "modelscope-token"}
	_, err := model.Resolve(spec)
	g.Expect(err).To(MatchError(ContainSubstring("secretRef")))
}
