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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/gateway"
)

const (
	gatewayPort            = 8080
	gatewayScalerPort      = 9090
	gatewayAggregatorPort  = 9091
	defaultGatewayReplicas = 1
	gatewayLabel           = "serving.noctaya.io/gateway"
	scalerLabel            = "serving.noctaya.io/scaler"
	backendSvcSuffix       = "-backend"
	gatewayScalerSvcSuffix = "-scaler"
	portNameHTTP           = "http"
	portNameGRPC           = "grpc"
	serviceKind            = "Service"
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

func BackendServiceName(svc *servingv1alpha1.LLMService) string {
	return svc.Name + backendSvcSuffix
}

func GatewayServiceName(svc *servingv1alpha1.LLMService) string { return svc.Name }

func GatewayScalerServiceName(svc *servingv1alpha1.LLMService) string {
	return svc.Name + gatewayScalerSvcSuffix
}

func GatewayScalerAddress(svc *servingv1alpha1.LLMService) string {
	return fmt.Sprintf("%s.%s.svc:%d", GatewayScalerServiceName(svc), svc.Namespace, gatewayScalerPort)
}

func GatewayDemandReportURL(svc *servingv1alpha1.LLMService) string {
	return fmt.Sprintf(
		"http://%s.%s.svc:%d%s",
		GatewayScalerServiceName(svc),
		svc.Namespace,
		gatewayAggregatorPort,
		gateway.DemandReportPath,
	)
}

func gatewaySelectorLabels(svc *servingv1alpha1.LLMService) map[string]string {
	return map[string]string{
		nameLabel:      svc.Name + "-gateway",
		managedByLabel: managedByValue,
		gatewayLabel:   svc.Name,
	}
}

func scalerSelectorLabels(svc *servingv1alpha1.LLMService) map[string]string {
	return map[string]string{
		nameLabel:      svc.Name + "-scaler",
		managedByLabel: managedByValue,
		scalerLabel:    svc.Name,
	}
}

// gatewayServiceLabels are the gateway Service's metadata labels: the selector labels
// plus the shared llmservice label used for external metrics discovery.
func gatewayServiceLabels(svc *servingv1alpha1.LLMService) map[string]string {
	l := gatewaySelectorLabels(svc)
	l[llmServiceLabel] = svc.Name
	return l
}

func BuildBackendService(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime) *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: serviceKind},
		ObjectMeta: metav1.ObjectMeta{Name: BackendServiceName(svc), Namespace: svc.Namespace, Labels: SelectorLabels(svc)},
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(svc),
			Ports: []corev1.ServicePort{{
				Name:       portNameHTTP,
				Port:       80,
				TargetPort: intstr.FromString(rt.Spec.Container.Port.Name),
				Protocol:   corev1.ProtocolTCP,
			}},
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

func BuildGatewayScalerService(svc *servingv1alpha1.LLMService, replicas int32) *corev1.Service {
	selector := gatewaySelectorLabels(svc)
	ports := []corev1.ServicePort{{
		Name:       portNameGRPC,
		Port:       gatewayScalerPort,
		TargetPort: intstr.FromInt(gatewayScalerPort),
		Protocol:   corev1.ProtocolTCP,
	}}
	if replicas > 1 {
		selector = scalerSelectorLabels(svc)
		ports = append(ports, corev1.ServicePort{
			Name:       portNameHTTP,
			Port:       gatewayAggregatorPort,
			TargetPort: intstr.FromInt(gatewayAggregatorPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: serviceKind},
		ObjectMeta: metav1.ObjectMeta{Name: GatewayScalerServiceName(svc), Namespace: svc.Namespace, Labels: gatewayServiceLabels(svc)},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
			Ports:    ports,
		},
	}
}

func BuildGatewayDeployment(svc *servingv1alpha1.LLMService, image string, replicas int32) (*appsv1.Deployment, error) {
	if replicas == 0 {
		replicas = defaultGatewayReplicas
	}
	if err := ValidateGatewayReplicas(int(replicas)); err != nil {
		return nil, err
	}
	backendURL := fmt.Sprintf("http://%s.%s.svc:80", BackendServiceName(svc), svc.Namespace)
	labels := gatewaySelectorLabels(svc)

	env := []corev1.EnvVar{
		{Name: gateway.EnvBackendURL, Value: backendURL},
		{Name: gateway.EnvListenAddr, Value: fmt.Sprintf(":%d", gatewayPort)},
	}
	ports := []corev1.ContainerPort{{
		Name: portNameHTTP, ContainerPort: gatewayPort, Protocol: corev1.ProtocolTCP,
	}}
	if replicas == 1 {
		env = append(env, corev1.EnvVar{
			Name: gateway.EnvScalerListenAddr, Value: fmt.Sprintf(":%d", gatewayScalerPort),
		})
		ports = append(ports, corev1.ContainerPort{
			Name: portNameGRPC, ContainerPort: gatewayScalerPort, Protocol: corev1.ProtocolTCP,
		})
	} else {
		env = append(env,
			corev1.EnvVar{Name: gateway.EnvDemandAggregator, Value: GatewayDemandReportURL(svc)},
			corev1.EnvVar{
				Name: gateway.EnvGatewayID,
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.uid",
				}},
			},
		)
	}
	if at := svc.Spec.Scaling.ActivationTimeout.Duration; at > 0 {
		env = append(env, corev1.EnvVar{Name: gateway.EnvActivationTimeout, Value: at.String()})
	}
	if cs := svc.Spec.Endpoint.ColdStart; cs.Mode != "" {
		env = append(env, corev1.EnvVar{Name: gateway.EnvColdStartMode, Value: cs.Mode})
		if hb := cs.HeartbeatInterval.Duration; hb > 0 {
			env = append(env, corev1.EnvVar{Name: gateway.EnvHeartbeatInterval, Value: hb.String()})
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
		}},
	}
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

func BuildGatewayScalerDeployment(
	svc *servingv1alpha1.LLMService,
	image string,
	gatewayReplicas int32,
) *appsv1.Deployment {
	if gatewayReplicas <= 1 {
		return nil
	}
	labels := scalerSelectorLabels(svc)
	replicas := int32(1)
	terminationGracePeriodSeconds := int64(30)
	probe := &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt(gatewayAggregatorPort)},
	}}
	pod := corev1.PodSpec{
		TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
		Containers: []corev1.Container{{
			Name:  "scaler",
			Image: image,
			Args:  []string{"--mode=aggregator"},
			Env: []corev1.EnvVar{
				{Name: gateway.EnvAggregatorAddr, Value: fmt.Sprintf(":%d", gatewayAggregatorPort)},
				{Name: gateway.EnvScalerListenAddr, Value: fmt.Sprintf(":%d", gatewayScalerPort)},
			},
			Ports: []corev1.ContainerPort{
				{Name: portNameGRPC, ContainerPort: gatewayScalerPort, Protocol: corev1.ProtocolTCP},
				{Name: portNameHTTP, ContainerPort: gatewayAggregatorPort, Protocol: corev1.ProtocolTCP},
			},
			ReadinessProbe: probe,
			LivenessProbe:  probe.DeepCopy(),
		}},
	}
	applyImagePullSecrets(&pod, svc)
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: appsAPIVersion, Kind: deploymentKind},
		ObjectMeta: metav1.ObjectMeta{Name: GatewayScalerServiceName(svc), Namespace: svc.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       pod,
			},
		},
	}
}
