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
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend/registry"
	"github.com/noctaya/noctaya/internal/backend/resources"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
	modelresolver "github.com/noctaya/noctaya/internal/model"
)

const (
	runtimeFileName = "serving_v1alpha1_inferenceruntime.yaml"
	serviceFileName = "serving_v1alpha1_llmservice.yaml"
)

type exampleKustomization struct {
	Resources []string `json:"resources"`
}

func decodeDeviceExample[T any](t *testing.T, name string) T {
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

func checkDeviceProfileFiles(t *testing.T, dir string) {
	t.Helper()
	kustomization := decodeDeviceExample[exampleKustomization](t, filepath.Join(dir, "kustomization.yaml"))
	for _, resource := range kustomization.Resources {
		if _, err := os.Stat(filepath.Join(dir, resource)); err != nil {
			t.Errorf("kustomization resource %s: %v", resource, err)
		}
	}
	for _, required := range []string{runtimeFileName, serviceFileName} {
		if !slices.Contains(kustomization.Resources, required) {
			t.Errorf("kustomization resources do not include %s", required)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list profile: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "kustomization.yaml" || name == runtimeFileName || name == serviceFileName {
			continue
		}
		if filepath.Ext(name) == ".yaml" {
			t.Errorf("unexpected profile YAML filename %s", name)
		}
	}
}

func checkDeviceRuntime(
	t *testing.T,
	adapters *registry.Registry,
	vendor string,
	runtime *servingv1alpha1.InferenceRuntime,
) backendruntime.BackendAdapter {
	t.Helper()
	if runtime.Spec.Vendor != vendor {
		t.Errorf("runtime vendor = %q, want %q", runtime.Spec.Vendor, vendor)
	}
	if runtime.Spec.Container.Image == "" {
		t.Error("runtime container image is empty")
	}
	if runtime.Spec.Accelerator.ResourceName == "" {
		t.Error("runtime accelerator resourceName is empty")
	}
	if len(runtime.Spec.Accelerator.NodeSelector) == 0 {
		t.Error("device profile has no accelerator node selector")
	}
	adapter, ok := adapters.Get(vendor)
	if !ok {
		t.Fatalf("no backend adapter registered for vendor %s", vendor)
	}
	return adapter
}

func checkDeviceService(
	t *testing.T,
	adapter backendruntime.BackendAdapter,
	runtime *servingv1alpha1.InferenceRuntime,
	service *servingv1alpha1.LLMService,
) {
	t.Helper()
	if service.Spec.Runtime.Name != runtime.Name {
		t.Errorf("%s references runtime %q, want %q", serviceFileName, service.Spec.Runtime.Name, runtime.Name)
	}
	if service.Spec.Scaling.Max < 1 {
		t.Errorf("maximum replicas = %d, want at least 1", service.Spec.Scaling.Max)
	}
	resolved, err := modelresolver.Resolve(service.Spec.Model)
	if err != nil {
		t.Fatalf("%s model: %v", serviceFileName, err)
	}
	deployment, err := resources.BuildBackendDeployment(adapter, service, runtime, resolved)
	if err != nil {
		t.Fatalf("%s deployment: %v", serviceFileName, err)
	}
	checkAcceleratorLimit(t, deployment.Spec.Template.Spec.Containers, runtime.Spec.Accelerator.ResourceName)

	if _, err := resources.BuildCachePVC(service); err != nil {
		t.Errorf("%s cache PVC: %v", serviceFileName, err)
	}
	if _, err := resources.BuildPrewarmJob(service, runtime, resolved); err != nil {
		t.Errorf("%s prewarm Job: %v", serviceFileName, err)
	}
	if _, err := resources.BuildScaledObject(service); err != nil {
		t.Errorf("%s ScaledObject: %v", serviceFileName, err)
	}
}

func checkAcceleratorLimit(t *testing.T, containers []corev1.Container, resourceName string) {
	t.Helper()
	for _, container := range containers {
		if container.Name != backendruntime.ServingContainerName {
			continue
		}
		if _, ok := container.Resources.Limits[corev1.ResourceName(resourceName)]; !ok {
			t.Errorf("serving container does not limit accelerator resource %q", resourceName)
		}
		return
	}
	t.Fatalf("no %q container rendered", backendruntime.ServingContainerName)
}

func checkDeviceExample(t *testing.T, adapters *registry.Registry, vendor, dir string) {
	t.Helper()
	checkDeviceProfileFiles(t, dir)
	runtime := decodeDeviceExample[servingv1alpha1.InferenceRuntime](t, filepath.Join(dir, runtimeFileName))
	adapter := checkDeviceRuntime(t, adapters, vendor, &runtime)
	service := decodeDeviceExample[servingv1alpha1.LLMService](t, filepath.Join(dir, serviceFileName))
	checkDeviceService(t, adapter, &runtime, &service)
}

func TestDeviceExampleAssociations(t *testing.T) {
	examplesRoot := filepath.Join("..", "..", "..", "examples")
	adapters := registry.New()
	vendors, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatalf("read %s: %v", examplesRoot, err)
	}

	profiles := 0
	for _, vendorEntry := range vendors {
		if !vendorEntry.IsDir() || slices.Contains([]string{"observability", "security"}, vendorEntry.Name()) {
			continue
		}
		vendor := vendorEntry.Name()
		vendorDir := filepath.Join(examplesRoot, vendor)
		devices, err := os.ReadDir(vendorDir)
		if err != nil {
			t.Fatalf("read %s: %v", vendorDir, err)
		}
		for _, device := range devices {
			if !device.IsDir() {
				continue
			}
			profiles++
			t.Run(vendor+"/"+device.Name(), func(t *testing.T) {
				checkDeviceExample(t, adapters, vendor, filepath.Join(vendorDir, device.Name()))
			})
		}
	}
	if profiles == 0 {
		t.Fatal("no device examples found")
	}
}
