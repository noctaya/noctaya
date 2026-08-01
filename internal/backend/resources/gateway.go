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

package resources

import (
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/gateway/proxy"
	"github.com/noctaya/noctaya/internal/gateway/scaler"
)

const (
	gatewayPort            = 8080
	defaultGatewayReplicas = 1
	gatewayLabel           = "serving.noctaya.io/gateway"
)

func ValidateGatewayReplicas(replicas int) error {
	if replicas < 1 {
		return fmt.Errorf("gateway replicas must be at least 1")
	}
	if int64(replicas) > int64(1<<31-1) {
		return fmt.Errorf("gateway replicas must fit in a Kubernetes int32 field")
	}
	return nil
}

func GatewayServiceName(svc *servingv1alpha1.LLMService) string { return svc.Name }

func gatewaySelectorLabels(svc *servingv1alpha1.LLMService) map[string]string {
	return map[string]string{
		nameLabel:      svc.Name + "-gateway",
		managedByLabel: managedByValue,
		gatewayLabel:   svc.Name,
	}
}

func gatewayServiceLabels(svc *servingv1alpha1.LLMService) map[string]string {
	labels := gatewaySelectorLabels(svc)
	labels[llmServiceLabel] = svc.Name
	return labels
}

func BuildGatewayDeployment(
	svc *servingv1alpha1.LLMService,
	image string,
	replicas int32,
) (*appsv1.Deployment, error) {
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("gateway image is required")
	}
	if replicas == 0 {
		replicas = defaultGatewayReplicas
	}
	if err := ValidateGatewayReplicas(int(replicas)); err != nil {
		return nil, err
	}
	backendURL := fmt.Sprintf("http://%s.%s.svc:80", BackendServiceName(svc), svc.Namespace)
	labels := gatewaySelectorLabels(svc)
	maxQueue := gatewayMaxQueue(svc)

	env := []corev1.EnvVar{
		{Name: proxy.EnvBackendURL, Value: backendURL},
		{Name: proxy.EnvListenAddr, Value: fmt.Sprintf(":%d", gatewayPort)},
		{Name: proxy.EnvMaxQueue, Value: strconv.FormatInt(int64(maxQueue), 10)},
	}
	ports := []corev1.ContainerPort{{
		Name: portNameHTTP, ContainerPort: gatewayPort, Protocol: corev1.ProtocolTCP,
	}}
	if replicas == 1 {
		env = append(env, corev1.EnvVar{
			Name: scaler.EnvListenAddr, Value: fmt.Sprintf(":%d", gatewayScalerPort),
		})
		ports = append(ports, corev1.ContainerPort{
			Name: portNameGRPC, ContainerPort: gatewayScalerPort, Protocol: corev1.ProtocolTCP,
		})
	} else {
		env = append(env,
			corev1.EnvVar{Name: proxy.EnvDemandAggregatorURL, Value: GatewayDemandReportURL(svc)},
			corev1.EnvVar{
				Name: proxy.EnvGatewayID,
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.uid",
				}},
			},
		)
	}
	if timeout := svc.Spec.Scaling.ActivationTimeout.Duration; timeout > 0 {
		env = append(env, corev1.EnvVar{Name: proxy.EnvActivationTimeout, Value: timeout.String()})
	}
	if coldStart := svc.Spec.Endpoint.ColdStart; coldStart.Mode != "" {
		env = append(env, corev1.EnvVar{Name: proxy.EnvColdStartMode, Value: coldStart.Mode})
		if heartbeat := coldStart.HeartbeatInterval.Duration; heartbeat > 0 {
			env = append(env, corev1.EnvVar{Name: proxy.EnvHeartbeatInterval, Value: heartbeat.String()})
		}
	}

	probe := &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(gatewayPort)},
	}}
	terminationGracePeriodSeconds := int64(30)
	pod := corev1.PodSpec{
		TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
		Containers: []corev1.Container{{
			Name:           "gateway",
			Image:          image,
			Env:            env,
			Ports:          ports,
			ReadinessProbe: probe,
			LivenessProbe:  probe.DeepCopy(),
			Resources:      gatewayResourceRequirements(svc.Spec.Endpoint.Resources),
		}},
	}
	if replicas > 1 {
		pod.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
				Weight: 100,
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{MatchLabels: gatewaySelectorLabels(svc)},
					TopologyKey:   corev1.LabelHostname,
				},
			}},
		}}
	}
	configureClientAuthentication(&pod, &pod.Containers[0], svc)
	if replicas == 1 {
		configureExternalScalerTLS(&pod, &pod.Containers[0], svc)
	} else {
		configureDemandAuth(&pod, &pod.Containers[0], svc)
	}
	hardenNoctayaContainer(&pod, &pod.Containers[0])
	applyImagePullSecrets(&pod, svc)

	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: appsAPIVersion, Kind: deploymentKind},
		ObjectMeta: metav1.ObjectMeta{Name: svc.Name + "-gateway", Namespace: svc.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       pod,
			},
		},
	}, nil
}

func BuildGatewayPodDisruptionBudget(
	svc *servingv1alpha1.LLMService,
	replicas int32,
) *policyv1.PodDisruptionBudget {
	if replicas <= 1 {
		return nil
	}
	minAvailable := intstr.FromInt32(1)
	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name + "-gateway",
			Namespace: svc.Namespace,
			Labels:    gatewayServiceLabels(svc),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     &metav1.LabelSelector{MatchLabels: gatewaySelectorLabels(svc)},
		},
	}
}

func BuildGatewayService(svc *servingv1alpha1.LLMService) *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: serviceKind},
		ObjectMeta: metav1.ObjectMeta{Name: GatewayServiceName(svc), Namespace: svc.Namespace, Labels: gatewayServiceLabels(svc)},
		Spec: corev1.ServiceSpec{
			Selector: gatewaySelectorLabels(svc),
			Ports: []corev1.ServicePort{{
				Name:       portNameHTTP,
				Port:       80,
				TargetPort: intstr.FromInt(gatewayPort),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

func gatewayMaxQueue(svc *servingv1alpha1.LLMService) int32 {
	if svc.Spec.Endpoint.MaxQueue > 0 {
		return svc.Spec.Endpoint.MaxQueue
	}
	return proxy.DefaultMaxQueue
}

func gatewayResourceRequirements(spec servingv1alpha1.GatewayResourceSpec) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: gatewayResourceList(spec.Requests),
		Limits:   gatewayResourceList(spec.Limits),
	}
}

func gatewayResourceList(spec servingv1alpha1.GatewayComputeSpec) corev1.ResourceList {
	var resources corev1.ResourceList
	if spec.CPU != nil {
		resources = corev1.ResourceList{}
		resources[corev1.ResourceCPU] = spec.CPU.DeepCopy()
	}
	if spec.Memory != nil {
		if resources == nil {
			resources = corev1.ResourceList{}
		}
		resources[corev1.ResourceMemory] = spec.Memory.DeepCopy()
	}
	return resources
}
