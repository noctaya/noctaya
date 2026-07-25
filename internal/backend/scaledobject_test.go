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
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend"
)

func scalingService() *servingv1alpha1.LLMService {
	return &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen3-8b", Namespace: "ai"},
		Spec: servingv1alpha1.LLMServiceSpec{
			Scaling: servingv1alpha1.ScalingSpec{Min: 0, Max: 3, Target: 10},
		},
	}
}

func TestScaledObjectMetricsAPITrigger(t *testing.T) {
	g := NewWithT(t)
	svc := scalingService()
	svc.Spec.Scaling.ScaleDownStabilization = metav1.Duration{Duration: 5 * time.Minute}
	so, err := backend.BuildScaledObject(svc, backend.ScalerModeMetricsAPI)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(so.GetAPIVersion()).To(Equal("keda.sh/v1alpha1"))
	g.Expect(so.GetKind()).To(Equal("ScaledObject"))

	spec := so.Object["spec"].(map[string]any)
	g.Expect(spec["minReplicaCount"]).To(Equal(int64(0)))
	g.Expect(spec["maxReplicaCount"]).To(Equal(int64(3)))
	g.Expect(spec["scaleTargetRef"]).To(HaveKeyWithValue("name", "qwen3-8b"))
	g.Expect(spec["cooldownPeriod"]).To(Equal(int64(300)))
	advanced := spec["advanced"].(map[string]any)
	hpa := advanced["horizontalPodAutoscalerConfig"].(map[string]any)
	behavior := hpa["behavior"].(map[string]any)
	scaleDown := behavior["scaleDown"].(map[string]any)
	g.Expect(scaleDown["stabilizationWindowSeconds"]).To(Equal(int64(300)))

	triggers := spec["triggers"].([]any)
	g.Expect(triggers).To(HaveLen(1))
	trig := triggers[0].(map[string]any)
	g.Expect(trig["type"]).To(Equal("metrics-api"))

	md := trig["metadata"].(map[string]any)
	g.Expect(md["url"]).To(Equal("http://qwen3-8b.ai.svc/noctaya/queue"))
	g.Expect(md["valueLocation"]).To(Equal("pending"))
	g.Expect(md["targetValue"]).To(Equal("10"))
	g.Expect(md["activationTargetValue"]).To(Equal("0"))
}

func TestScaledObjectRejectsUnimplementedMetric(t *testing.T) {
	g := NewWithT(t)
	svc := scalingService()
	svc.Spec.Scaling.Metric = "kvCacheUtil"
	_, err := backend.BuildScaledObject(svc, backend.ScalerModeMetricsAPI)
	g.Expect(err).To(MatchError(ContainSubstring("kvCacheUtil")))

	svc.Spec.Scaling.Metric = "queueDepth"
	_, err = backend.BuildScaledObject(svc, backend.ScalerModeMetricsAPI)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestScaledObjectRejectsInvalidScaleDownStabilization(t *testing.T) {
	for _, window := range []time.Duration{-time.Second, 1500 * time.Millisecond, 61 * time.Minute} {
		t.Run(window.String(), func(t *testing.T) {
			g := NewWithT(t)
			svc := scalingService()
			svc.Spec.Scaling.ScaleDownStabilization = metav1.Duration{Duration: window}
			_, err := backend.BuildScaledObject(svc, backend.ScalerModeMetricsAPI)
			g.Expect(err).To(MatchError(ContainSubstring("from 0s to 1h")))
		})
	}
}

func TestScaledObjectPreservesZeroScaleDownStabilization(t *testing.T) {
	g := NewWithT(t)
	so, err := backend.BuildScaledObject(scalingService(), backend.ScalerModeMetricsAPI)
	g.Expect(err).NotTo(HaveOccurred())
	spec := so.Object["spec"].(map[string]any)
	g.Expect(spec["cooldownPeriod"]).To(Equal(int64(0)))
	advanced := spec["advanced"].(map[string]any)
	hpa := advanced["horizontalPodAutoscalerConfig"].(map[string]any)
	behavior := hpa["behavior"].(map[string]any)
	scaleDown := behavior["scaleDown"].(map[string]any)
	g.Expect(scaleDown["stabilizationWindowSeconds"]).To(Equal(int64(0)))
}

func TestScaledObjectExternalPushTrigger(t *testing.T) {
	g := NewWithT(t)
	so, err := backend.BuildScaledObject(scalingService(), backend.ScalerModeExternalPush)
	g.Expect(err).NotTo(HaveOccurred())

	spec := so.Object["spec"].(map[string]any)
	triggers := spec["triggers"].([]any)
	g.Expect(triggers).To(HaveLen(1))
	trigger := triggers[0].(map[string]any)
	g.Expect(trigger["type"]).To(Equal("external-push"))
	metadata := trigger["metadata"].(map[string]any)
	g.Expect(metadata).To(Equal(map[string]any{
		"scalerAddress": "qwen3-8b-scaler.ai.svc:9090",
		"metricName":    "pending",
		"targetValue":   "10",
	}))
}

func TestParseScalerMode(t *testing.T) {
	g := NewWithT(t)
	for _, value := range []string{"", "metrics-api", "external-push"} {
		_, err := backend.ParseScalerMode(value)
		g.Expect(err).NotTo(HaveOccurred())
	}
	_, err := backend.ParseScalerMode("unknown")
	g.Expect(err).To(MatchError(ContainSubstring("unsupported scaler mode")))
}
