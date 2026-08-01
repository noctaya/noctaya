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

package llmservice

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	backendresources "github.com/noctaya/noctaya/internal/backend/resources"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

type desiredResources struct {
	scaledObject      *unstructured.Unstructured
	backendDeployment *appsv1.Deployment
	backendService    *corev1.Service
	gatewayDeployment *appsv1.Deployment
	gatewayService    *corev1.Service
	gatewayPDB        *policyv1.PodDisruptionBudget
	scalerDeployment  *appsv1.Deployment
	scalerService     *corev1.Service
	cachePVC          *corev1.PersistentVolumeClaim
	prewarmJob        *batchv1.Job
	demandSecret      *corev1.Secret
}

func (r *LLMServiceReconciler) renderDesired(
	svc *servingv1alpha1.LLMService,
	rt *servingv1alpha1.InferenceRuntime,
	adapter backendruntime.BackendAdapter,
	model backendruntime.ResolvedModel,
) (*desiredResources, string, error) {
	desired := &desiredResources{}
	var err error

	desired.scaledObject, err = backendresources.BuildScaledObject(svc)
	if err != nil {
		return nil, scaledObjectKind, err
	}
	desired.backendDeployment, err = backendresources.BuildBackendDeployment(adapter, svc, rt, model)
	if err != nil {
		return nil, "Render", err
	}
	desired.backendService = backendresources.BuildBackendService(svc, rt)
	desired.gatewayDeployment, err = backendresources.BuildGatewayDeployment(svc, r.GatewayImage, r.GatewayReplicas)
	if err != nil {
		return nil, "GatewayConfig", err
	}
	desired.gatewayService = backendresources.BuildGatewayService(svc)
	desired.gatewayPDB = backendresources.BuildGatewayPodDisruptionBudget(svc, r.GatewayReplicas)
	desired.scalerDeployment = backendresources.BuildGatewayScalerDeployment(svc, r.GatewayImage, r.GatewayReplicas)
	desired.scalerService = backendresources.BuildGatewayScalerService(svc, r.GatewayReplicas)
	desired.cachePVC, err = backendresources.BuildCachePVC(svc)
	if err != nil {
		return nil, "Cache", err
	}
	desired.prewarmJob, err = backendresources.BuildPrewarmJob(svc, rt, model)
	if err != nil {
		return nil, "Prewarm", err
	}
	desired.demandSecret, err = backendresources.BuildDemandAuthSecret(svc, r.GatewayReplicas)
	if err != nil {
		return nil, "DemandAuthentication", err
	}
	return desired, "", nil
}

func (r *LLMServiceReconciler) applyDesired(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
	desired *desiredResources,
) (string, bool, error) {
	if err := r.apply(ctx, svc, desired.scaledObject); err != nil {
		if meta.IsNoMatchError(err) {
			return "AutoscalingUnavailable", false,
				fmt.Errorf("KEDA ScaledObject CRD is required before deploying an LLMService: %w", err)
		}
		return "ApplyScaledObject", false, err
	}

	if desired.demandSecret != nil {
		if err := r.ensureCreated(ctx, svc, desired.demandSecret); err != nil {
			return "ApplyDemandAuthentication", true, err
		}
		if err := backendresources.ValidateDemandAuthSecret(desired.demandSecret); err != nil {
			return "DemandAuthentication", true, err
		}
	}
	if desired.cachePVC != nil {
		if err := r.ensureCreated(ctx, svc, desired.cachePVC); err != nil {
			return "ApplyCachePVC", true, err
		}
	}
	if desired.prewarmJob != nil {
		if err := r.ensureCreated(ctx, svc, desired.prewarmJob); err != nil {
			return "ApplyPrewarmJob", true, err
		}
	}
	if err := r.apply(ctx, svc, desired.backendDeployment); err != nil {
		return "ApplyDeployment", true, err
	}
	if err := r.apply(ctx, svc, desired.backendService); err != nil {
		return "ApplyBackendService", true, err
	}
	if err := r.apply(ctx, svc, desired.gatewayDeployment); err != nil {
		return "ApplyGateway", true, err
	}
	if err := r.apply(ctx, svc, desired.gatewayService); err != nil {
		return "ApplyGatewayService", true, err
	}
	if desired.gatewayPDB != nil {
		if err := r.apply(ctx, svc, desired.gatewayPDB); err != nil {
			return "ApplyGatewayPodDisruptionBudget", true, err
		}
	} else if err := r.deleteOwned(ctx, svc, &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: svc.Name + "-gateway", Namespace: svc.Namespace},
	}); err != nil {
		return "DeleteGatewayPodDisruptionBudget", true, err
	}
	if desired.scalerDeployment != nil {
		if err := r.apply(ctx, svc, desired.scalerDeployment); err != nil {
			return "ApplyGatewayScaler", true, err
		}
	}
	if err := r.apply(ctx, svc, desired.scalerService); err != nil {
		return "ApplyGatewayScalerService", true, err
	}
	if desired.scalerDeployment == nil {
		if reason, err := r.deleteObsoleteScaler(ctx, svc); err != nil {
			return reason, true, err
		}
	}
	return "", true, nil
}

func (r *LLMServiceReconciler) deleteObsoleteScaler(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
) (string, error) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name:      backendresources.GatewayScalerServiceName(svc),
		Namespace: svc.Namespace,
	}}
	if err := r.deleteOwned(ctx, svc, deployment); err != nil {
		return "DeleteGatewayScaler", err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      backendresources.DemandAuthSecretName(svc),
		Namespace: svc.Namespace,
	}}
	if err := r.deleteOwned(ctx, svc, secret); err != nil {
		return "DeleteDemandAuthentication", err
	}
	return "", nil
}
