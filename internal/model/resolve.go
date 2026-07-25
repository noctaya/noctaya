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

// Package model resolves an LLMService model spec into a runtime-loadable model.
package model

import (
	"fmt"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
)

// Resolve maps a model source to the path and environment consumed by the runtime.
func Resolve(model servingv1alpha1.ModelSpec) (backend.ResolvedModel, error) {
	if model.Source == nil || model.Source.URI == "" {
		if model.CatalogRef != "" {
			return backend.ResolvedModel{}, fmt.Errorf("model.catalogRef resolution is not implemented yet; set model.source.uri")
		}
		return backend.ResolvedModel{}, fmt.Errorf("model.source.uri is required")
	}

	if model.Source.SecretRef != nil {
		return backend.ResolvedModel{}, fmt.Errorf("model.source.secretRef is not supported yet; use a public model source or remove it")
	}

	scheme, ref, ok := strings.Cut(model.Source.URI, "://")
	if !ok || ref == "" {
		return backend.ResolvedModel{}, fmt.Errorf("invalid model uri %q: expected scheme://reference", model.Source.URI)
	}

	switch scheme {
	case "hf", "huggingface":
		return backend.ResolvedModel{Path: ref, Source: "hf"}, nil
	case "modelscope":
		return backend.ResolvedModel{
			Path:   ref,
			Source: "modelscope",
			Env:    []corev1.EnvVar{{Name: "VLLM_USE_MODELSCOPE", Value: "true"}},
		}, nil
	case "pvc":
		pvcName, subpath, _ := strings.Cut(ref, "/")
		if errs := validation.IsDNS1123Subdomain(pvcName); len(errs) > 0 {
			return backend.ResolvedModel{}, fmt.Errorf("invalid pvc uri %q: expected pvc://<claim>[/<subpath>]", model.Source.URI)
		}
		if subpath != "" && (path.IsAbs(subpath) || path.Clean(subpath) != subpath || subpath == ".." || strings.HasPrefix(subpath, "../")) {
			return backend.ResolvedModel{}, fmt.Errorf("invalid pvc uri %q: subpath must stay within the model volume", model.Source.URI)
		}
		return backend.ResolvedModel{Path: subpath, Source: "pvc", PVC: pvcName}, nil
	default:
		return backend.ResolvedModel{}, fmt.Errorf("model uri scheme %q is not supported yet (use hf://, modelscope://, or pvc://)", scheme)
	}
}
