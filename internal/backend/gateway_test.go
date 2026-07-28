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
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
)

func gatewaySvc() *servingv1alpha1.LLMService {
	return &servingv1alpha1.LLMService{ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"}}
}

func TestGatewayReplicasDefaultAndValidation(t *testing.T) {
	g := NewWithT(t)

	dep, err := backend.BuildGatewayDeployment(gatewaySvc(), "img", 0)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dep.Spec.Replicas).NotTo(BeNil())
	g.Expect(*dep.Spec.Replicas).To(Equal(int32(1)))

	g.Expect(backend.ValidateGatewayReplicas(1)).To(Succeed())
	for _, replicas := range []int{-1, 0, 2} {
		g.Expect(backend.ValidateGatewayReplicas(replicas)).To(MatchError(
			"gateway replicas must be 1 until demand aggregation is available",
		))
	}
	for _, replicas := range []int32{-1, 2} {
		_, err := backend.BuildGatewayDeployment(gatewaySvc(), "img", replicas)
		g.Expect(err).To(MatchError("gateway replicas must be 1 until demand aggregation is available"))
	}
}

func TestGatewayCarriesImagePullSecrets(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	svc.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "regcred"}}
	dep, err := backend.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dep.Spec.Template.Spec.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: "regcred"}))
}

func TestGatewayPointsAtBackendService(t *testing.T) {
	g := NewWithT(t)
	dep, err := backend.BuildGatewayDeployment(gatewaySvc(), "img", 1)
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

func TestGatewayExposesInternalScaler(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	dep, err := backend.BuildGatewayDeployment(svc, "img", 1)
	g.Expect(err).NotTo(HaveOccurred())
	container := dep.Spec.Template.Spec.Containers[0]
	g.Expect(container.Env).To(ContainElement(corev1.EnvVar{
		Name:  "NOCTAYA_SCALER_LISTEN_ADDR",
		Value: ":9090",
	}))
	g.Expect(container.Ports).To(ContainElement(corev1.ContainerPort{
		Name: "grpc", ContainerPort: 9090, Protocol: corev1.ProtocolTCP,
	}))

	service := backend.BuildGatewayScalerService(svc)
	g.Expect(service.Name).To(Equal("qwen3-8b-scaler"))
	g.Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
	g.Expect(service.Spec.Ports).To(HaveLen(1))
	g.Expect(service.Spec.Ports[0].Name).To(Equal("grpc"))
	g.Expect(service.Spec.Ports[0].Port).To(Equal(int32(9090)))
	g.Expect(dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(HaveValue(Equal(int64(30))))
}

func TestServicesExposeMetricsDiscoveryContract(t *testing.T) {
	g := NewWithT(t)
	svc := gatewaySvc()
	rt := &servingv1alpha1.InferenceRuntime{Spec: servingv1alpha1.InferenceRuntimeSpec{
		Container: servingv1alpha1.RuntimeContainer{
			Port: servingv1alpha1.RuntimePort{Name: "runtime-http", ContainerPort: 8000},
		},
	}}

	services := []*corev1.Service{
		backend.BuildBackendService(svc, rt),
		backend.BuildGatewayService(svc),
	}
	for _, service := range services {
		g.Expect(service.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "noctaya"))
		g.Expect(service.Labels).To(HaveKeyWithValue("serving.noctaya.io/llmservice", svc.Name))
		g.Expect(service.Spec.Ports).To(HaveLen(1))
		g.Expect(service.Spec.Ports[0].Name).To(Equal("http"))
	}
}

type serviceMonitorFixture struct {
	Spec struct {
		Endpoints []struct {
			Path string `json:"path"`
			Port string `json:"port"`
		} `json:"endpoints"`
		Selector struct {
			MatchLabels      map[string]string `json:"matchLabels"`
			MatchExpressions []struct {
				Key      string `json:"key"`
				Operator string `json:"operator"`
			} `json:"matchExpressions"`
		} `json:"selector"`
	} `json:"spec"`
}

func TestOptionalObservabilityAssetsMatchDiscoveryContract(t *testing.T) {
	g := NewWithT(t)
	monitor := decodeExample[serviceMonitorFixture](t,
		"../../examples/observability/prometheus/servicemonitor.yaml")
	g.Expect(monitor.Spec.Endpoints).To(HaveLen(1))
	g.Expect(monitor.Spec.Endpoints[0].Path).To(Equal("/metrics"))
	g.Expect(monitor.Spec.Endpoints[0].Port).To(Equal("http"))
	g.Expect(monitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "noctaya"))
	g.Expect(monitor.Spec.Selector.MatchExpressions).To(ContainElement(And(
		HaveField("Key", "serving.noctaya.io/llmservice"),
		HaveField("Operator", "Exists"),
	)))

	dashboard := decodeExample[map[string]any](t,
		"../../examples/observability/grafana/noctaya-overview.json")
	g.Expect(dashboard).To(HaveKeyWithValue("uid", "noctaya-overview"))
}
