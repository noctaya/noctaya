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
	"strconv"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend/resources"
)

func gatewaySvc() *servingv1alpha1.LLMService {
	return &servingv1alpha1.LLMService{ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"}}
}

func TestGatewayReplicasDefaultAndValidation(t *testing.T) {
	g := NewWithT(t)

	dep, err := resources.BuildGatewayDeployment(gatewaySvc(), "img", 0)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dep.Spec.Replicas).NotTo(BeNil())
	g.Expect(*dep.Spec.Replicas).To(Equal(int32(1)))

	g.Expect(resources.ValidateGatewayReplicas(1)).To(Succeed())
	g.Expect(resources.ValidateGatewayReplicas(2)).To(Succeed())
	for _, replicas := range []int{-1, 0} {
		g.Expect(resources.ValidateGatewayReplicas(replicas)).To(MatchError(
			"gateway replicas must be at least 1",
		))
	}
	if strconv.IntSize == 64 {
		g.Expect(resources.ValidateGatewayReplicas(1 << 31)).To(MatchError(
			"gateway replicas must fit in a Kubernetes int32 field",
		))
	}
	for _, replicas := range []int32{-1} {
		_, err := resources.BuildGatewayDeployment(gatewaySvc(), "img", replicas)
		g.Expect(err).To(MatchError("gateway replicas must be at least 1"))
	}
	dep, err = resources.BuildGatewayDeployment(gatewaySvc(), "img", 2)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(*dep.Spec.Replicas).To(Equal(int32(2)))

	_, err = resources.BuildGatewayDeployment(gatewaySvc(), " ", 1)
	g.Expect(err).To(MatchError("gateway image is required"))
}

func TestGatewayCarriesImagePullSecrets(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	svc.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "regcred"}}
	dep, err := resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dep.Spec.Template.Spec.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: "regcred"}))
	scaler := resources.BuildGatewayScalerDeployment(svc, "img", 2)
	g.Expect(scaler.Spec.Template.Spec.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: "regcred"}))
}

func TestNoctayaDataPlanePodsUseRestrictedSecurityDefaults(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	gateway, err := resources.BuildGatewayDeployment(svc, "img", 2)
	g.Expect(err).NotTo(HaveOccurred())
	scaler := resources.BuildGatewayScalerDeployment(svc, "img", 2)

	for _, pod := range []corev1.PodSpec{gateway.Spec.Template.Spec, scaler.Spec.Template.Spec} {
		g.Expect(pod.AutomountServiceAccountToken).To(HaveValue(BeFalse()))
		g.Expect(pod.SecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
		g.Expect(pod.SecurityContext.RunAsUser).To(HaveValue(Equal(int64(65532))))
		g.Expect(pod.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		security := pod.Containers[0].SecurityContext
		g.Expect(security.AllowPrivilegeEscalation).To(HaveValue(BeFalse()))
		g.Expect(security.ReadOnlyRootFilesystem).To(HaveValue(BeTrue()))
		g.Expect(security.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
	}
}

func TestGatewayPointsAtBackendService(t *testing.T) {
	g := NewWithT(t)
	dep, err := resources.BuildGatewayDeployment(gatewaySvc(), "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	env := dep.Spec.Template.Spec.Containers[0].Env
	var backendURL string
	for _, e := range env {
		if e.Name == "NOCTAYA_BACKEND_URL" {
			backendURL = e.Value
		}
	}
	g.Expect(backendURL).To(Equal("http://qwen3-8b-backend.ai.svc:80"))
}

func TestGatewayQueueCapacityDefaultAndOverride(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()

	deployment, err := resources.BuildGatewayDeployment(svc, "img", 2)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{
		Name: "NOCTAYA_MAX_QUEUE", Value: "100",
	}))
	g.Expect(resources.BuildGatewayScalerDeployment(svc, "img", 2).
		Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{
		Name: "NOCTAYA_MAX_GATEWAY_DEMAND", Value: "100",
	}))

	svc.Spec.Endpoint.MaxQueue = 7
	deployment, err = resources.BuildGatewayDeployment(svc, "img", 2)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{
		Name: "NOCTAYA_MAX_QUEUE", Value: "7",
	}))
	g.Expect(resources.BuildGatewayScalerDeployment(svc, "img", 2).
		Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{
		Name: "NOCTAYA_MAX_GATEWAY_DEMAND", Value: "7",
	}))
}

func TestGatewayComputeResources(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()

	deployment, err := resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Resources).
		To(Equal(corev1.ResourceRequirements{}))

	requestCPU := resource.MustParse("100m")
	requestMemory := resource.MustParse("128Mi")
	svc.Spec.Endpoint.Resources.Requests = servingv1alpha1.GatewayComputeSpec{
		CPU: &requestCPU, Memory: &requestMemory,
	}
	deployment, err = resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	containerResources := deployment.Spec.Template.Spec.Containers[0].Resources
	g.Expect(containerResources.Requests).To(Equal(corev1.ResourceList{
		corev1.ResourceCPU: requestCPU, corev1.ResourceMemory: requestMemory,
	}))
	g.Expect(containerResources.Limits).To(BeNil())

	limitCPU := resource.MustParse("1")
	limitMemory := resource.MustParse("256Mi")
	svc.Spec.Endpoint.Resources.Limits = servingv1alpha1.GatewayComputeSpec{
		CPU: &limitCPU, Memory: &limitMemory,
	}
	deployment, err = resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	containerResources = deployment.Spec.Template.Spec.Containers[0].Resources
	g.Expect(containerResources.Requests).To(Equal(corev1.ResourceList{
		corev1.ResourceCPU: requestCPU, corev1.ResourceMemory: requestMemory,
	}))
	g.Expect(containerResources.Limits).To(Equal(corev1.ResourceList{
		corev1.ResourceCPU: limitCPU, corev1.ResourceMemory: limitMemory,
	}))
}

func TestGatewayClientAuthenticationSecretMount(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()

	deployment, err := resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deployment.Spec.Template.Spec.Volumes).To(BeEmpty())
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Env).
		NotTo(ContainElement(HaveField("Name", "NOCTAYA_CLIENT_API_KEY_FILE")))

	svc.Spec.Endpoint.Authentication = &servingv1alpha1.EndpointAuthenticationSpec{
		SecretRef: servingv1alpha1.SecretKeyReference{Name: "model-client", Key: "token"},
	}
	deployment, err = resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	pod := deployment.Spec.Template.Spec
	g.Expect(pod.Volumes).To(ContainElement(And(
		HaveField("Name", "client-auth"),
		HaveField("VolumeSource.Secret.SecretName", "model-client"),
		HaveField("VolumeSource.Secret.Items", []corev1.KeyToPath{{Key: "token", Path: "api-key"}}),
	)))
	g.Expect(pod.Containers[0].VolumeMounts).To(ContainElement(corev1.VolumeMount{
		Name: "client-auth", MountPath: "/var/run/noctaya/client-auth", ReadOnly: true,
	}))
	g.Expect(pod.Containers[0].Env).To(ContainElement(corev1.EnvVar{
		Name: "NOCTAYA_CLIENT_API_KEY_FILE", Value: "/var/run/noctaya/client-auth/api-key",
	}))
}

func TestMultipleGatewayAvailabilityPolicy(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()

	single, err := resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(single.Spec.Template.Spec.Affinity).To(BeNil())
	g.Expect(resources.BuildGatewayPodDisruptionBudget(svc, 1)).To(BeNil())

	multiple, err := resources.BuildGatewayDeployment(svc, "img", 2)
	g.Expect(err).NotTo(HaveOccurred())
	antiAffinity := multiple.Spec.Template.Spec.Affinity.PodAntiAffinity.
		PreferredDuringSchedulingIgnoredDuringExecution
	g.Expect(antiAffinity).To(HaveLen(1))
	g.Expect(antiAffinity[0].Weight).To(Equal(int32(100)))
	g.Expect(antiAffinity[0].PodAffinityTerm.TopologyKey).To(Equal(corev1.LabelHostname))
	g.Expect(antiAffinity[0].PodAffinityTerm.LabelSelector.MatchLabels).
		To(Equal(multiple.Spec.Selector.MatchLabels))

	pdb := resources.BuildGatewayPodDisruptionBudget(svc, 2)
	g.Expect(pdb.APIVersion).To(Equal("policy/v1"))
	g.Expect(pdb.Kind).To(Equal("PodDisruptionBudget"))
	g.Expect(pdb.Name).To(Equal("qwen3-8b-gateway"))
	g.Expect(pdb.Spec.MinAvailable.Type).To(Equal(intstr.Int))
	g.Expect(pdb.Spec.MinAvailable.IntVal).To(Equal(int32(1)))
	g.Expect(pdb.Spec.Selector.MatchLabels).To(Equal(multiple.Spec.Selector.MatchLabels))
	g.Expect(pdb.Spec.UnhealthyPodEvictionPolicy).To(BeNil())
	g.Expect(pdb.Status).To(Equal(policyv1.PodDisruptionBudgetStatus{}))
}

func TestSingleGatewayExposesCoLocatedScaler(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	dep, err := resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	container := dep.Spec.Template.Spec.Containers[0]
	g.Expect(container.Env).To(ContainElement(corev1.EnvVar{
		Name:  "NOCTAYA_SCALER_LISTEN_ADDR",
		Value: ":9090",
	}))
	g.Expect(container.Ports).To(ContainElement(corev1.ContainerPort{
		Name: "grpc", ContainerPort: 9090, Protocol: corev1.ProtocolTCP,
	}))

	service := resources.BuildGatewayScalerService(svc, 1)
	g.Expect(service.Name).To(Equal("qwen3-8b-scaler"))
	g.Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
	g.Expect(service.Spec.Ports).To(HaveLen(1))
	g.Expect(service.Spec.Ports[0].Name).To(Equal("grpc"))
	g.Expect(service.Spec.Ports[0].Port).To(Equal(int32(9090)))
	g.Expect(service.Spec.Selector).To(HaveKeyWithValue("serving.noctaya.io/gateway", svc.Name))
	g.Expect(resources.BuildGatewayScalerDeployment(svc, "img", 1)).To(BeNil())
	g.Expect(dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(HaveValue(Equal(int64(30))))
}

func TestMultipleGatewaysPublishToAggregateScaler(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	dep, err := resources.BuildGatewayDeployment(svc, "img", 2)
	g.Expect(err).NotTo(HaveOccurred())
	container := dep.Spec.Template.Spec.Containers[0]
	g.Expect(container.Env).NotTo(ContainElement(HaveField("Name", "NOCTAYA_SCALER_LISTEN_ADDR")))
	g.Expect(container.Ports).NotTo(ContainElement(HaveField("Name", "grpc")))
	g.Expect(container.Env).To(ContainElement(corev1.EnvVar{
		Name:  "NOCTAYA_DEMAND_AGGREGATOR_URL",
		Value: "http://qwen3-8b-scaler.ai.svc:9091/v1/demand",
	}))
	g.Expect(container.Env).To(ContainElement(And(
		HaveField("Name", "NOCTAYA_GATEWAY_ID"),
		HaveField("ValueFrom.FieldRef.FieldPath", "metadata.uid"),
	)))
	expectDemandAuthMount(g, dep.Spec.Template.Spec)

	scaler := resources.BuildGatewayScalerDeployment(svc, "img", 2)
	g.Expect(scaler).NotTo(BeNil())
	g.Expect(scaler.Spec.Replicas).To(HaveValue(Equal(int32(1))))
	g.Expect(scaler.Spec.Template.Labels).To(HaveKeyWithValue("serving.noctaya.io/scaler", svc.Name))
	scalerContainer := scaler.Spec.Template.Spec.Containers[0]
	g.Expect(scalerContainer.Args).To(Equal([]string{"--mode=aggregator"}))
	g.Expect(scalerContainer.Ports).To(ConsistOf(
		corev1.ContainerPort{Name: "grpc", ContainerPort: 9090, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "http", ContainerPort: 9091, Protocol: corev1.ProtocolTCP},
	))
	expectDemandAuthMount(g, scaler.Spec.Template.Spec)
	g.Expect(scalerContainer.Env).To(ContainElements(
		corev1.EnvVar{Name: "NOCTAYA_MAX_GATEWAY_MEMBERS", Value: "16"},
		corev1.EnvVar{Name: "NOCTAYA_MAX_GATEWAY_DEMAND", Value: "100"},
	))

	service := resources.BuildGatewayScalerService(svc, 2)
	g.Expect(service.Spec.Selector).To(HaveKeyWithValue("serving.noctaya.io/scaler", svc.Name))
	g.Expect(service.Spec.Ports).To(HaveLen(2))
	g.Expect(service.Spec.Ports).To(ContainElement(HaveField("Name", "grpc")))
	g.Expect(service.Spec.Ports).To(ContainElement(HaveField("Name", "http")))
}

func TestExternalScalerMutualTLSIsMountedOnlyWhereScalerRuns(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	svc.Spec.Scaling.ExternalScaler = &servingv1alpha1.ExternalScalerSpec{
		TLS: &servingv1alpha1.ExternalScalerTLSSpec{
			ServerSecretName: "scaler-server-tls",
			AuthenticationRef: servingv1alpha1.KEDAAuthenticationReference{
				Name: "scaler-client-tls",
			},
		},
	}

	single, err := resources.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	expectScalerTLSMount(g, single.Spec.Template.Spec)

	multiple, err := resources.BuildGatewayDeployment(svc, "img", 2)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(multiple.Spec.Template.Spec.Volumes).NotTo(
		ContainElement(HaveField("Name", "external-scaler-tls")),
	)
	g.Expect(multiple.Spec.Template.Spec.Containers[0].VolumeMounts).NotTo(
		ContainElement(HaveField("Name", "external-scaler-tls")),
	)
	g.Expect(multiple.Spec.Template.Spec.Containers[0].Env).NotTo(
		ContainElement(HaveField("Name", "NOCTAYA_SCALER_TLS_CERT_FILE")),
	)

	aggregateScaler := resources.BuildGatewayScalerDeployment(svc, "img", 2)
	expectScalerTLSMount(g, aggregateScaler.Spec.Template.Spec)
}

func TestDemandAuthenticationSecretIsCreatedOnlyForMultipleGateways(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()

	single, err := resources.BuildDemandAuthSecret(svc, 1)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(single).To(BeNil())

	secret, err := resources.BuildDemandAuthSecret(svc, 2)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secret.Name).To(Equal("qwen3-8b-demand-auth"))
	g.Expect(secret.Data["token"]).To(HaveLen(43))
	g.Expect(resources.ValidateDemandAuthSecret(secret)).To(Succeed())

	delete(secret.Data, "token")
	g.Expect(resources.ValidateDemandAuthSecret(secret)).To(MatchError(
		ContainSubstring(`is missing key "token"`),
	))
	secret.Data["token"] = []byte("short")
	g.Expect(resources.ValidateDemandAuthSecret(secret)).To(MatchError(
		ContainSubstring("must contain at least 32 characters"),
	))
}

func expectDemandAuthMount(g *WithT, pod corev1.PodSpec) {
	g.Expect(pod.Volumes).To(ContainElement(And(
		HaveField("Name", "demand-auth"),
		HaveField("VolumeSource.Secret.SecretName", "qwen3-8b-demand-auth"),
	)))
	container := pod.Containers[0]
	g.Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
		Name:      "demand-auth",
		MountPath: "/var/run/noctaya/demand-auth",
		ReadOnly:  true,
	}))
	g.Expect(container.Env).To(ContainElement(corev1.EnvVar{
		Name: "NOCTAYA_DEMAND_AUTH_TOKEN_FILE", Value: "/var/run/noctaya/demand-auth/token",
	}))
}

func expectScalerTLSMount(g *WithT, pod corev1.PodSpec) {
	g.Expect(pod.Volumes).To(ContainElement(And(
		HaveField("Name", "external-scaler-tls"),
		HaveField("VolumeSource.Secret.SecretName", "scaler-server-tls"),
	)))
	container := pod.Containers[0]
	g.Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
		Name:      "external-scaler-tls",
		MountPath: "/var/run/noctaya/scaler-tls",
		ReadOnly:  true,
	}))
	g.Expect(container.Env).To(ContainElements(
		corev1.EnvVar{
			Name: "NOCTAYA_SCALER_TLS_CERT_FILE", Value: "/var/run/noctaya/scaler-tls/tls.crt",
		},
		corev1.EnvVar{
			Name: "NOCTAYA_SCALER_TLS_KEY_FILE", Value: "/var/run/noctaya/scaler-tls/tls.key",
		},
		corev1.EnvVar{
			Name: "NOCTAYA_SCALER_TLS_CLIENT_CA_FILE", Value: "/var/run/noctaya/scaler-tls/ca.crt",
		},
	))
}

func TestServicesExposeMetricsDiscoveryContract(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	runtime := &servingv1alpha1.InferenceRuntime{Spec: servingv1alpha1.InferenceRuntimeSpec{
		Container: servingv1alpha1.RuntimeContainer{
			Port: servingv1alpha1.RuntimePort{Name: "runtime-http", ContainerPort: 8000},
		},
	}}

	services := []*corev1.Service{
		resources.BuildBackendService(svc, runtime),
		resources.BuildGatewayService(svc),
	}
	for _, service := range services {
		g.Expect(service.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "noctaya"))
		g.Expect(service.Labels).To(HaveKeyWithValue("serving.noctaya.io/llmservice", svc.Name))
		g.Expect(service.Spec.Ports).To(HaveLen(1))
		g.Expect(service.Spec.Ports[0].Name).To(Equal("http"))
	}

	scalerService := resources.BuildGatewayScalerService(svc, 2)
	g.Expect(scalerService.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "noctaya"))
	g.Expect(scalerService.Labels).To(HaveKeyWithValue("serving.noctaya.io/llmservice", svc.Name))
	g.Expect(scalerService.Spec.Ports).To(ContainElement(HaveField("Name", "http")))
}
