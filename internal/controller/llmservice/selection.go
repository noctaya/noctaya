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

package llmservice

import (
	"context"
	"fmt"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

func (r *LLMServiceReconciler) resolveRuntime(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
) (*servingv1alpha1.InferenceRuntime, error) {
	selection := svc.Spec.Runtime
	if selection.Name != "" {
		var runtime servingv1alpha1.InferenceRuntime
		if err := r.Get(ctx, client.ObjectKey{Name: selection.Name}, &runtime); err != nil {
			return nil, fmt.Errorf("get InferenceRuntime %q: %w", selection.Name, err)
		}
		return &runtime, nil
	}
	if selection.Selector != nil && len(selection.Selector.Vendor) > 0 {
		var runtimes servingv1alpha1.InferenceRuntimeList
		if err := r.List(ctx, &runtimes); err != nil {
			return nil, err
		}
		return pickByVendor(runtimes.Items, selection.Selector.Vendor)
	}
	return nil, fmt.Errorf("spec.runtime: set either name or selector.vendor")
}

func pickByVendor(
	runtimes []servingv1alpha1.InferenceRuntime,
	vendors []string,
) (*servingv1alpha1.InferenceRuntime, error) {
	for _, vendor := range vendors {
		var best *servingv1alpha1.InferenceRuntime
		var tied []string
		for i := range runtimes {
			if runtimes[i].Spec.Vendor != vendor {
				continue
			}
			if best == nil || runtimes[i].Spec.Priority > best.Spec.Priority {
				best = &runtimes[i]
				tied = []string{runtimes[i].Name}
			} else if runtimes[i].Spec.Priority == best.Spec.Priority {
				tied = append(tied, runtimes[i].Name)
			}
		}
		if best != nil {
			if len(tied) > 1 {
				slices.Sort(tied)
				return nil, fmt.Errorf(
					"multiple InferenceRuntimes match vendor %q at priority %d: %v; set spec.runtime.name",
					vendor,
					best.Spec.Priority,
					tied,
				)
			}
			return best, nil
		}
	}
	return nil, fmt.Errorf("no InferenceRuntime matches vendors %v", vendors)
}
