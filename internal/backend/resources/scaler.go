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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/gateway/demand"
	"github.com/noctaya/noctaya/internal/gateway/scaler"
)

const (
	gatewayScalerPort      = 9090
	gatewayAggregatorPort  = 9091
	scalerLabel            = "serving.noctaya.io/scaler"
	gatewayScalerSvcSuffix = "-scaler"
	portNameGRPC           = "grpc"
	scalerTLSVolumeName    = "external-scaler-tls"
	scalerTLSMountPath     = "/var/run/noctaya/scaler-tls"
	scalerTLSCertPath      = scalerTLSMountPath + "/tls.crt"
	scalerTLSKeyPath       = scalerTLSMountPath + "/tls.key"
	scalerTLSCAPath        = scalerTLSMountPath + "/ca.crt"
	demandAuthSecretSuffix = "-demand-auth"
	demandAuthVolumeName   = "demand-auth"
	demandAuthMountPath    = "/var/run/noctaya/demand-auth"
	demandAuthTokenKey     = "token"
	demandAuthTokenPath    = demandAuthMountPath + "/" + demandAuthTokenKey
)

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
		scaler.DemandReportPath,
	)
}

func DemandAuthSecretName(svc *servingv1alpha1.LLMService) string {
	return svc.Name + demandAuthSecretSuffix
}

func BuildDemandAuthSecret(
	svc *servingv1alpha1.LLMService,
	gatewayReplicas int32,
) (*corev1.Secret, error) {
	if gatewayReplicas <= 1 {
		return nil, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate demand authentication token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DemandAuthSecretName(svc),
			Namespace: svc.Namespace,
			Labels:    SelectorLabels(svc),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{demandAuthTokenKey: []byte(token)},
	}, nil
}

func ValidateDemandAuthSecret(secret *corev1.Secret) error {
	token, found := secret.Data[demandAuthTokenKey]
	if !found {
		return fmt.Errorf("demand authentication Secret %s is missing key %q", secret.Name, demandAuthTokenKey)
	}
	if err := demand.ValidateAuthToken(string(token)); err != nil {
		return fmt.Errorf("demand authentication Secret %s is invalid: %w", secret.Name, err)
	}
	return nil
}

func scalerSelectorLabels(svc *servingv1alpha1.LLMService) map[string]string {
	return map[string]string{
		nameLabel:      svc.Name + "-scaler",
		managedByLabel: managedByValue,
		scalerLabel:    svc.Name,
	}
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
				{Name: scaler.EnvAggregatorListenAddr, Value: fmt.Sprintf(":%d", gatewayAggregatorPort)},
				{Name: scaler.EnvListenAddr, Value: fmt.Sprintf(":%d", gatewayScalerPort)},
				{Name: scaler.EnvMaxGatewayMembers, Value: strconv.FormatInt(max(16, int64(gatewayReplicas)*4), 10)},
				{Name: scaler.EnvMaxGatewayDemand, Value: strconv.FormatInt(int64(gatewayMaxQueue(svc)), 10)},
			},
			Ports: []corev1.ContainerPort{
				{Name: portNameGRPC, ContainerPort: gatewayScalerPort, Protocol: corev1.ProtocolTCP},
				{Name: portNameHTTP, ContainerPort: gatewayAggregatorPort, Protocol: corev1.ProtocolTCP},
			},
			ReadinessProbe: probe,
			LivenessProbe:  probe.DeepCopy(),
		}},
	}
	configureDemandAuth(&pod, &pod.Containers[0], svc)
	configureExternalScalerTLS(&pod, &pod.Containers[0], svc)
	hardenNoctayaContainer(&pod, &pod.Containers[0])
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

func configureDemandAuth(
	pod *corev1.PodSpec,
	container *corev1.Container,
	svc *servingv1alpha1.LLMService,
) {
	mode := int32(0o440)
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: demandAuthVolumeName,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName:  DemandAuthSecretName(svc),
			DefaultMode: &mode,
		}},
	})
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name: demandAuthVolumeName, MountPath: demandAuthMountPath, ReadOnly: true,
	})
	container.Env = append(container.Env, corev1.EnvVar{
		Name: demand.EnvAuthTokenFile, Value: demandAuthTokenPath,
	})
}

func configureExternalScalerTLS(
	pod *corev1.PodSpec,
	container *corev1.Container,
	svc *servingv1alpha1.LLMService,
) {
	externalScaler := svc.Spec.Scaling.ExternalScaler
	if externalScaler == nil || externalScaler.TLS == nil {
		return
	}
	mode := int32(0o440)
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: scalerTLSVolumeName,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName:  externalScaler.TLS.ServerSecretName,
			DefaultMode: &mode,
		}},
	})
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name: scalerTLSVolumeName, MountPath: scalerTLSMountPath, ReadOnly: true,
	})
	container.Env = append(container.Env,
		corev1.EnvVar{Name: scaler.EnvTLSCertFile, Value: scalerTLSCertPath},
		corev1.EnvVar{Name: scaler.EnvTLSKeyFile, Value: scalerTLSKeyPath},
		corev1.EnvVar{Name: scaler.EnvTLSClientCAFile, Value: scalerTLSCAPath},
	)
}
