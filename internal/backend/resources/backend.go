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

// Package resources builds Kubernetes objects for an LLMService.
package resources

import (
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

const (
	managedByLabel   = "app.kubernetes.io/managed-by"
	managedByValue   = "noctaya"
	nameLabel        = "app.kubernetes.io/name"
	llmServiceLabel  = "serving.noctaya.io/llmservice"
	runtimeLabel     = "serving.noctaya.io/runtime"
	appsAPIVersion   = "apps/v1"
	deploymentKind   = "Deployment"
	serviceKind      = "Service"
	backendSvcSuffix = "-backend"
	portNameHTTP     = "http"

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

func BackendServiceName(svc *servingv1alpha1.LLMService) string {
	return svc.Name + backendSvcSuffix
}

func BuildBackendDeployment(
	adapter backendruntime.BackendAdapter,
	svc *servingv1alpha1.LLMService,
	runtime *servingv1alpha1.InferenceRuntime,
	model backendruntime.ResolvedModel,
) (*appsv1.Deployment, error) {
	pod, err := adapter.PodSpec(svc, runtime, model)
	if err != nil {
		return nil, err
	}
	accel, err := adapter.Accelerator(svc, runtime)
	if err != nil {
		return nil, err
	}
	applyAccelerator(&pod, accel)

	artifacts, err := planCache(svc)
	if err != nil {
		return nil, err
	}
	applyCache(&pod, artifacts)

	// The operator intentionally does NOT set .spec.replicas: KEDA's HPA owns the
	// backend replica count (0..N, including scale-to-zero). On first create the API
	// server defaults it to 1 until KEDA takes over.
	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: appsAPIVersion, Kind: deploymentKind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Labels:    podLabels(svc, runtime),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(svc)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels(svc, runtime)},
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

func BuildBackendService(
	svc *servingv1alpha1.LLMService,
	runtime *servingv1alpha1.InferenceRuntime,
) *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: serviceKind},
		ObjectMeta: metav1.ObjectMeta{Name: BackendServiceName(svc), Namespace: svc.Namespace, Labels: SelectorLabels(svc)},
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(svc),
			Ports: []corev1.ServicePort{{
				Name:       portNameHTTP,
				Port:       80,
				TargetPort: intstr.FromString(runtime.Spec.Container.Port.Name),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
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
func applyAccelerator(pod *corev1.PodSpec, accel backendruntime.AcceleratorRequest) {
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
		if pod.Containers[i].Name != backendruntime.ServingContainerName {
			continue
		}
		if pod.Containers[i].Resources.Limits == nil {
			pod.Containers[i].Resources.Limits = corev1.ResourceList{}
		}
		maps.Copy(pod.Containers[i].Resources.Limits, accel.Resources)
	}
}
