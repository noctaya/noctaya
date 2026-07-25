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
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
	"github.com/noctaya/noctaya/internal/backend/registry"
	modelresolver "github.com/noctaya/noctaya/internal/model"
)

type exampleKustomization struct {
	Resources []string `json:"resources"`
}

var expectedNodeSelectors = map[string]map[string]string{
	"ascend/310p-duo": {
		"accelerator":                       "huawei-Ascend310P",
		"serving.noctaya.io/ascend-product": "atlas-300i-duo",
	},
	"ascend/310p-pro": {
		"accelerator":                       "huawei-Ascend310P",
		"serving.noctaya.io/ascend-product": "atlas-300i-pro",
	},
	"ascend/910b3": {
		"accelerator":                       "huawei-Ascend910",
		"serving.noctaya.io/ascend-product": "ascend-910b3",
	},
	"nvidia/a10": {
		"nvidia.com/gpu.product": "NVIDIA-A10",
	},
	"nvidia/a100": {
		"nvidia.com/gpu.product": "NVIDIA-A100",
	},
}

func decodeExample[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(name) //nolint:gosec // paths come from the repository fixture tree
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	jsonData, err := utilyaml.ToJSON(data)
	if err != nil {
		t.Fatalf("convert %s to JSON: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(jsonData, &out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return out
}

func checkNodeSelectors(t *testing.T, profileName string, actual map[string]string) {
	t.Helper()
	expected, ok := expectedNodeSelectors[profileName]
	if !ok {
		t.Fatalf("no expected node selectors registered for %s", profileName)
	}
	for key, value := range expected {
		if actual[key] != value {
			t.Errorf("runtime node selector %s = %q, want %q", key, actual[key], value)
		}
	}
}

func TestDeviceExampleAssociations(t *testing.T) {
	examplesRoot := filepath.Join("..", "..", "examples")
	adapters := registry.New()
	vendors, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatalf("read %s: %v", examplesRoot, err)
	}
	for _, vendorEntry := range vendors {
		if !vendorEntry.IsDir() || vendorEntry.Name() == "observability" {
			continue
		}
		vendor := vendorEntry.Name()
		vendorDir := filepath.Join(examplesRoot, vendor)
		devices, err := os.ReadDir(vendorDir)
		if err != nil {
			t.Fatalf("read %s: %v", vendorDir, err)
		}
		var profilePriority *int32
		for _, device := range devices {
			if !device.IsDir() {
				continue
			}
			profileName := vendor + "/" + device.Name()
			t.Run(profileName, func(t *testing.T) {
				dir := filepath.Join(vendorDir, device.Name())
				runtimeFile := "serving_v1alpha1_inferenceruntime.yaml"
				serviceFile := "serving_v1alpha1_llmservice.yaml"
				kustomization := decodeExample[exampleKustomization](t, filepath.Join(dir, "kustomization.yaml"))
				for _, resource := range kustomization.Resources {
					if _, err := os.Stat(filepath.Join(dir, resource)); err != nil {
						t.Errorf("kustomization resource %s: %v", resource, err)
					}
				}
				for _, required := range []string{runtimeFile, serviceFile} {
					if !slices.Contains(kustomization.Resources, required) {
						t.Errorf("kustomization resources do not include %s", required)
					}
				}

				runtime := decodeExample[servingv1alpha1.InferenceRuntime](t, filepath.Join(dir, runtimeFile))
				if runtime.Spec.Vendor != vendor {
					t.Errorf("runtime vendor = %q, want %q", runtime.Spec.Vendor, vendor)
				}
				checkNodeSelectors(t, profileName, runtime.Spec.Accelerator.NodeSelector)
				adapter, ok := adapters.Get(vendor)
				if !ok {
					t.Fatalf("no backend adapter registered for vendor %s", vendor)
				}
				if profilePriority == nil {
					priority := runtime.Spec.Priority
					profilePriority = &priority
				} else if runtime.Spec.Priority != *profilePriority {
					t.Errorf("runtime priority = %d, want %d so vendor selection cannot silently prefer one device profile", runtime.Spec.Priority, *profilePriority)
				}

				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatalf("list profile: %v", err)
				}
				for _, entry := range entries {
					name := entry.Name()
					if entry.IsDir() || name == "kustomization.yaml" || name == runtimeFile || name == serviceFile {
						continue
					}
					if filepath.Ext(name) == ".yaml" {
						t.Errorf("unexpected profile YAML filename %s", name)
					}
				}

				service := decodeExample[servingv1alpha1.LLMService](t, filepath.Join(dir, serviceFile))
				if service.Spec.Runtime.Name != runtime.Name {
					t.Errorf("%s references runtime %q, want %q", serviceFile, service.Spec.Runtime.Name, runtime.Name)
				}
				resolved, err := modelresolver.Resolve(service.Spec.Model)
				if err != nil {
					t.Fatalf("%s model: %v", serviceFile, err)
				}
				if _, err := backend.BuildDeployment(adapter, &service, &runtime, resolved); err != nil {
					t.Errorf("%s deployment: %v", serviceFile, err)
				}
				if _, err := backend.BuildCachePVC(&service); err != nil {
					t.Errorf("%s cache PVC: %v", serviceFile, err)
				}
				if _, err := backend.BuildPrewarmJob(&service, &runtime, resolved); err != nil {
					t.Errorf("%s prewarm Job: %v", serviceFile, err)
				}
				if _, err := backend.BuildScaledObject(&service, backend.ScalerModeMetricsAPI); err != nil {
					t.Errorf("%s ScaledObject: %v", serviceFile, err)
				}
			})
		}
	}
}
