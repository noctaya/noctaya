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

package backend

import (
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

const ServingContainerName = "serving"

const (
	managedByLabel  = "app.kubernetes.io/managed-by"
	managedByValue  = "noctaya"
	nameLabel       = "app.kubernetes.io/name"
	llmServiceLabel = "serving.noctaya.io/llmservice"
	runtimeLabel    = "serving.noctaya.io/runtime"
	appsAPIVersion  = "apps/v1"
	deploymentKind  = "Deployment"

	volcanoQueueAnnotation = "scheduling.volcano.sh/queue-name"
)

// SelectorLabels are the immutable labels identifying one LLMService's pods.
func SelectorLabels(svc *servingv1alpha1.LLMService) map[string]string {
	return map[string]string{
		nameLabel:       svc.Name,
		managedByLabel:  managedByValue,
		llmServiceLabel: svc.Name,
	}
}

func podLabels(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) map[string]string {
	l := SelectorLabels(svc)
	l[runtimeLabel] = rt.Name
	return l
}

func BuildDeployment(a BackendAdapter, svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime, m ResolvedModel) (*appsv1.Deployment, error) {
	pod, err := a.PodSpec(svc, rt, m)
	if err != nil {
		return nil, err
	}
	accel, err := a.Accelerator(svc, rt)
	if err != nil {
		return nil, err
	}
	applyAccelerator(&pod, accel)

	art, err := planCache(svc)
	if err != nil {
		return nil, err
	}
	applyCache(&pod, art)

	// The operator intentionally does NOT set .spec.replicas: KEDA's HPA owns the
	// backend replica count (0..N, including scale-to-zero). On first create the API
	// server defaults it to 1 until KEDA takes over.
	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: appsAPIVersion, Kind: deploymentKind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Labels:    podLabels(svc, rt),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(svc)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels(svc, rt)},
				Spec:       pod,
			},
		},
	}
	// Volcano's podgroup controller derives the queue from this pod annotation;
	// Noctaya never creates PodGroups itself (reuse the scheduler, don't replace it).
	if accel.Queue != "" {
		dep.Spec.Template.Annotations = map[string]string{volcanoQueueAnnotation: accel.Queue}
	}
	return dep, nil
}

// applyImagePullSecrets sets the LLMService's pull secrets on a rendered pod so private /
// air-gapped backend, gateway, and prewarm images can be pulled. Secrets are namespaced,
// which is why this lives on the namespaced LLMService rather than the cluster-scoped runtime.
func applyImagePullSecrets(pod *corev1.PodSpec, svc *servingv1alpha1.LLMService) {
	pod.ImagePullSecrets = append(pod.ImagePullSecrets, svc.Spec.ImagePullSecrets...)
}

// applyAccelerator merges the accelerator request into the serving container and pod.
// Extended (device-plugin) resources are set as limits only; Kubernetes mirrors them
// into requests automatically.
func applyAccelerator(pod *corev1.PodSpec, accel AcceleratorRequest) {
	if len(accel.NodeSelector) > 0 {
		if pod.NodeSelector == nil {
			pod.NodeSelector = map[string]string{}
		}
		maps.Copy(pod.NodeSelector, accel.NodeSelector)
	}
	pod.Tolerations = append(pod.Tolerations, accel.Tolerations...)
	if accel.SchedulerName != "" {
		pod.SchedulerName = accel.SchedulerName
	}
	for i := range pod.Containers {
		if pod.Containers[i].Name != ServingContainerName {
			continue
		}
		if pod.Containers[i].Resources.Limits == nil {
			pod.Containers[i].Resources.Limits = corev1.ResourceList{}
		}
		maps.Copy(pod.Containers[i].Resources.Limits, accel.Resources)
	}
}
