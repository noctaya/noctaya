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

package runtime

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

// WholeDeviceAccelerator translates one runtime profile into a Kubernetes device request.
func WholeDeviceAccelerator(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) (AcceleratorRequest, error) {
	name := rt.Spec.Accelerator.ResourceName
	if name == "" {
		return AcceleratorRequest{}, fmt.Errorf("runtime %q has no accelerator.resourceName", rt.Name)
	}
	scheduler := rt.Spec.Accelerator.Scheduler
	if scheduler.Queue != "" && scheduler.Name != "volcano" {
		return AcceleratorRequest{}, fmt.Errorf("runtime %q sets accelerator.scheduler.queue without scheduler.name=volcano", rt.Name)
	}
	if svc.Spec.Resources.Fraction != nil {
		return AcceleratorRequest{}, fmt.Errorf("resources.fraction is not supported in v0; runtime %q serves whole devices, set resources.accelerators instead", rt.Name)
	}
	count := svc.Spec.Resources.Accelerators
	if count <= 0 {
		count = 1
	}
	return AcceleratorRequest{
		Resources:     corev1.ResourceList{corev1.ResourceName(name): *resource.NewQuantity(int64(count), resource.DecimalSI)},
		NodeSelector:  rt.Spec.Accelerator.NodeSelector,
		Tolerations:   rt.Spec.Accelerator.Tolerations,
		SchedulerName: scheduler.Name,
		Queue:         scheduler.Queue,
	}, nil
}

// HostMount describes a read-only host path required by a vendor adapter.
type HostMount struct {
	Name string
	Path string
}

// AddHostMounts exposes required host driver paths to the serving container.
func AddHostMounts(pod *corev1.PodSpec, mounts []HostMount) {
	for _, mount := range mounts {
		pod.Volumes = append(pod.Volumes, corev1.Volume{
			Name:         mount.Name,
			VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: mount.Path}},
		})
		for i := range pod.Containers {
			if pod.Containers[i].Name != ServingContainerName {
				continue
			}
			pod.Containers[i].VolumeMounts = append(pod.Containers[i].VolumeMounts, corev1.VolumeMount{
				Name: mount.Name, MountPath: mount.Path, ReadOnly: true,
			})
		}
	}
}
