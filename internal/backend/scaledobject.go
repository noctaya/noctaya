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
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

const (
	kedaAPIVersion         = "keda.sh/v1alpha1"
	kedaKind               = "ScaledObject"
	defaultPollingInterval = 5 // seconds; also bounds fallback polling for external-push
	defaultMaxReplicas     = 1
	defaultTarget          = 10
	maxHPAStabilization    = time.Hour
)

type ScalerMode string

const (
	ScalerModeMetricsAPI   ScalerMode = "metrics-api"
	ScalerModeExternalPush ScalerMode = "external-push"
)

func ParseScalerMode(value string) (ScalerMode, error) {
	switch ScalerMode(value) {
	case "", ScalerModeMetricsAPI:
		return ScalerModeMetricsAPI, nil
	case ScalerModeExternalPush:
		return ScalerModeExternalPush, nil
	default:
		return "", fmt.Errorf("unsupported scaler mode %q; use %q or %q", value, ScalerModeMetricsAPI, ScalerModeExternalPush)
	}
}

func ScaledObjectName(svc *servingv1alpha1.LLMService) string { return svc.Name }

// BuildScaledObject renders a KEDA ScaledObject that scales the backend Deployment
// (0..N, including scale-to-zero) on the gateway's pending-request count.
func BuildScaledObject(svc *servingv1alpha1.LLMService, scalerMode ScalerMode) (*unstructured.Unstructured, error) {
	if m := svc.Spec.Scaling.Metric; m != "" && m != "queueDepth" {
		return nil, fmt.Errorf("scaling.metric %q is not supported in v0; only queueDepth is wired to the autoscaler", m)
	}
	mode, err := ParseScalerMode(string(scalerMode))
	if err != nil {
		return nil, err
	}
	target := svc.Spec.Scaling.Target
	if target <= 0 {
		target = defaultTarget
	}
	maxReplicas := svc.Spec.Scaling.Max
	if maxReplicas <= 0 {
		maxReplicas = defaultMaxReplicas
	}
	trigger := metricsAPITrigger(svc, target)
	if mode == ScalerModeExternalPush {
		trigger = externalPushTrigger(svc, target)
	}

	spec := map[string]any{
		"scaleTargetRef":  map[string]any{"name": svc.Name},
		"minReplicaCount": int64(svc.Spec.Scaling.Min),
		"maxReplicaCount": int64(maxReplicas),
		"pollingInterval": int64(defaultPollingInterval),
		"triggers":        []any{trigger},
	}
	window := svc.Spec.Scaling.ScaleDownStabilization.Duration
	if window < 0 || window%time.Second != 0 || window > maxHPAStabilization {
		return nil, fmt.Errorf("scaling.scaleDownStabilization must be a whole number of seconds from 0s to 1h")
	}
	cooldown := int64(window / time.Second)
	spec["cooldownPeriod"] = cooldown
	spec["advanced"] = map[string]any{
		"horizontalPodAutoscalerConfig": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": cooldown,
				},
			},
		},
	}

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(kedaAPIVersion)
	obj.SetKind(kedaKind)
	obj.SetName(ScaledObjectName(svc))
	obj.SetNamespace(svc.Namespace)
	obj.SetLabels(SelectorLabels(svc))
	obj.Object["spec"] = spec
	return obj, nil
}

func metricsAPITrigger(svc *servingv1alpha1.LLMService, target int32) map[string]any {
	return map[string]any{
		"type": string(ScalerModeMetricsAPI),
		"metadata": map[string]any{
			"url":                   fmt.Sprintf("http://%s.%s.svc/noctaya/queue", GatewayServiceName(svc), svc.Namespace),
			"valueLocation":         "pending",
			"targetValue":           strconv.Itoa(int(target)),
			"activationTargetValue": "0", // any pending request wakes from zero
		},
	}
}

func externalPushTrigger(svc *servingv1alpha1.LLMService, target int32) map[string]any {
	return map[string]any{
		"type": string(ScalerModeExternalPush),
		"metadata": map[string]any{
			"scalerAddress": GatewayScalerAddress(svc),
			"metricName":    "pending",
			"targetValue":   strconv.Itoa(int(target)),
		},
	}
}
