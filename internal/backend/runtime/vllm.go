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
	"math"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

const (
	ServingContainerName = "serving"
	sharedMemoryVolume   = "dshm"
	modelStoreVolume     = "model-store"
	// ModelPVCMountPath is where a pvc:// model store is mounted in the serving pod.
	ModelPVCMountPath = "/models"
)

// RenderVLLMPodSpec builds the vendor-neutral vLLM serving pod: one container with
// templated args/env, the runtime's model-load-aware probes, CPU/memory from the
// service, and a memory-backed /dev/shm (vLLM crashes on the default 64Mi). Accelerator
// resources and scheduling are layered on by the backend Deployment builder via the adapter's
// Accelerator. Vendor adapters call this and add only their vendor-specific extras.
func RenderVLLMPodSpec(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime, m ResolvedModel) (corev1.PodSpec, error) {
	// For a pvc:// model the weights live on a mounted PVC, so --model is a local path
	// (mount + subpath) rather than a repo id.
	modelPath := m.Path
	if m.PVC != "" {
		modelPath = path.Join(ModelPVCMountPath, m.Path)
	}
	data := TemplateData{
		Model:   ModelData{Path: modelPath},
		Service: ServiceData{Name: svc.Name, Namespace: svc.Namespace},
	}

	args := append(append([]string{}, rt.Spec.Container.Args...), svc.Spec.Runtime.ArgsOverride...)
	renderedArgs, err := RenderAll(args, data)
	if err != nil {
		return corev1.PodSpec{}, err
	}

	env, err := renderEnv(rt.Spec.Container.Env, data)
	if err != nil {
		return corev1.PodSpec{}, err
	}
	env = append(env, m.Env...)

	port := rt.Spec.Container.Port
	container := corev1.Container{
		Name:           ServingContainerName,
		Image:          rt.Spec.Container.Image,
		Args:           renderedArgs,
		Env:            env,
		Ports:          []corev1.ContainerPort{{Name: port.Name, ContainerPort: port.ContainerPort, Protocol: corev1.ProtocolTCP}},
		Resources:      computeResources(svc),
		ReadinessProbe: rt.Spec.Health.Readiness.DeepCopy(),
		LivenessProbe:  rt.Spec.Health.Liveness.DeepCopy(),
		StartupProbe:   rt.Spec.Health.Startup.DeepCopy(),
		VolumeMounts:   []corev1.VolumeMount{{Name: sharedMemoryVolume, MountPath: "/dev/shm"}},
	}

	// Load-gated readiness: even if the runtime omits probes, never let a slow model
	// load trip liveness or route traffic to a not-yet-loaded pod.
	if container.ReadinessProbe == nil {
		container.ReadinessProbe = healthProbe(port.Name)
	}
	if container.StartupProbe == nil {
		container.StartupProbe = defaultStartupProbe(port.Name)
	}

	pod := corev1.PodSpec{
		AutomountServiceAccountToken: ptr.To(false),
		Containers:                   []corev1.Container{container},
		Volumes: []corev1.Volume{{
			Name:         sharedMemoryVolume,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}},
		}},
	}
	if gp := rt.Spec.Lifecycle.TerminationGracePeriodSeconds; gp != nil {
		pod.TerminationGracePeriodSeconds = gp
	}
	if m.PVC != "" {
		mountModelStore(&pod, m.PVC)
	}
	pod.ImagePullSecrets = append(pod.ImagePullSecrets, svc.Spec.ImagePullSecrets...)
	applyDrain(&pod, svc, rt)
	return pod, nil
}

// applyDrain wires graceful scale-down: a preStop sleep keeps the pod alive (and removed
// from Service endpoints, so no new traffic) while in-flight streams finish before SIGTERM.
// terminationGracePeriodSeconds is widened to cover the drain so the kubelet doesn't cut it short.
func applyDrain(pod *corev1.PodSpec, svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) {
	if !rt.Spec.Lifecycle.PreStopDrain {
		return
	}
	drain := svc.Spec.Scaling.DrainTimeout.Duration
	if drain <= 0 {
		return
	}
	secs := int64(math.Ceil(drain.Seconds()))
	for i := range pod.Containers {
		if pod.Containers[i].Name != ServingContainerName {
			continue
		}
		pod.Containers[i].Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", fmt.Sprintf("sleep %d", secs)}},
			},
		}
	}
	grace := secs + 10
	if pod.TerminationGracePeriodSeconds == nil || *pod.TerminationGracePeriodSeconds < grace {
		pod.TerminationGracePeriodSeconds = &grace
	}
}

// mountModelStore mounts an existing PVC (pvc:// source) read-only at ModelPVCMountPath
// on the serving container, so vLLM loads pre-staged weights with no download.
func mountModelStore(pod *corev1.PodSpec, pvcName string) {
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: modelStoreVolume,
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: pvcName, ReadOnly: true,
		}},
	})
	for i := range pod.Containers {
		if pod.Containers[i].Name != ServingContainerName {
			continue
		}
		pod.Containers[i].VolumeMounts = append(pod.Containers[i].VolumeMounts, corev1.VolumeMount{
			Name: modelStoreVolume, MountPath: ModelPVCMountPath, ReadOnly: true,
		})
	}
}

func computeResources(svc *servingv1alpha1.LLMService) corev1.ResourceRequirements {
	r := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	if cpu := svc.Spec.Resources.CPU; cpu != nil {
		r.Requests[corev1.ResourceCPU] = *cpu
	}
	if mem := svc.Spec.Resources.Memory; mem != nil {
		r.Requests[corev1.ResourceMemory] = *mem
		r.Limits[corev1.ResourceMemory] = *mem
	}
	if len(r.Requests) == 0 {
		r.Requests = nil
	}
	if len(r.Limits) == 0 {
		r.Limits = nil
	}
	return r
}

func healthProbe(portName string) *corev1.Probe {
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString(portName)},
	}}
}

func defaultStartupProbe(portName string) *corev1.Probe {
	p := healthProbe(portName)
	p.PeriodSeconds = 10
	p.FailureThreshold = 60 // ~10 min budget for weight loading
	return p
}

func renderEnv(in []corev1.EnvVar, data TemplateData) ([]corev1.EnvVar, error) {
	out := make([]corev1.EnvVar, 0, len(in))
	for _, e := range in {
		if e.Value != "" {
			v, err := Render(e.Value, data)
			if err != nil {
				return nil, err
			}
			e.Value = v
		}
		out = append(out, e)
	}
	return out, nil
}
