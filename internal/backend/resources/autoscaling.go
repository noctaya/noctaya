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
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

const (
	kedaAPIVersion         = "keda.sh/v1alpha1"
	kedaKind               = "ScaledObject"
	defaultPollingInterval = 5 // seconds; bounds fallback activity checks and metric-based scale-out
	defaultMaxReplicas     = 1
	defaultTarget          = 10
	maxHPAStabilization    = time.Hour
)

func ScaledObjectName(svc *servingv1alpha1.LLMService) string { return svc.Name }

// BuildScaledObject renders a KEDA ScaledObject that scales the backend Deployment
// (0..N, including scale-to-zero) on the gateway's pending-request count.
func BuildScaledObject(svc *servingv1alpha1.LLMService) (*unstructured.Unstructured, error) {
	if m := svc.Spec.Scaling.Metric; m != "" && m != "queueDepth" {
		return nil, fmt.Errorf("scaling.metric %q is not supported in v0; only queueDepth is wired to the autoscaler", m)
	}
	if err := validateExternalScalerTLS(svc.Spec.Scaling.ExternalScaler); err != nil {
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

	spec := map[string]any{
		"scaleTargetRef":  map[string]any{"name": svc.Name},
		"minReplicaCount": int64(svc.Spec.Scaling.Min),
		"maxReplicaCount": int64(maxReplicas),
		"pollingInterval": int64(defaultPollingInterval),
		"triggers":        []any{externalPushTrigger(svc, target)},
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

func externalPushTrigger(svc *servingv1alpha1.LLMService, target int32) map[string]any {
	trigger := map[string]any{
		"type": "external-push",
		"metadata": map[string]any{
			"scalerAddress": GatewayScalerAddress(svc),
			"metricName":    "pending",
			"targetValue":   strconv.Itoa(int(target)),
		},
	}
	if scaler := svc.Spec.Scaling.ExternalScaler; scaler != nil && scaler.TLS != nil {
		ref := map[string]any{"name": scaler.TLS.AuthenticationRef.Name}
		if kind := scaler.TLS.AuthenticationRef.Kind; kind != "" {
			ref["kind"] = kind
		}
		trigger["authenticationRef"] = ref
	}
	return trigger
}

func validateExternalScalerTLS(spec *servingv1alpha1.ExternalScalerSpec) error {
	if spec == nil || spec.TLS == nil {
		return nil
	}
	if spec.TLS.ServerSecretName == "" {
		return fmt.Errorf("scaling.externalScaler.tls.serverSecretName is required")
	}
	if spec.TLS.AuthenticationRef.Name == "" {
		return fmt.Errorf("scaling.externalScaler.tls.authenticationRef.name is required")
	}
	switch kind := spec.TLS.AuthenticationRef.Kind; kind {
	case "", "TriggerAuthentication", "ClusterTriggerAuthentication":
		return nil
	default:
		return fmt.Errorf(
			"scaling.externalScaler.tls.authenticationRef.kind %q is invalid",
			kind,
		)
	}
}
