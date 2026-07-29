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
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

const (
	conditionAutoscalingReady = "AutoscalingReady"
	conditionDegraded         = "Degraded"
	conditionReady            = "Ready"
)

func (r *LLMServiceReconciler) updateStatus(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
	runtimeName string,
	deployment *appsv1.Deployment,
	pods []corev1.Pod,
) (ctrl.Result, error) {
	oldStatus := svc.Status.DeepCopy()
	previousDegraded := meta.FindStatusCondition(oldStatus.Conditions, conditionDegraded)
	observation := observeBackend(deployment, pods)

	phase := servingv1alpha1.PhasePending
	switch {
	case deployment.Status.ReadyReplicas > 0:
		phase = servingv1alpha1.PhaseReady
	case observation.degraded:
		phase = servingv1alpha1.PhaseDegraded
	case deployment.Status.Replicas > 0 || hasActiveBackendPod(pods):
		phase = servingv1alpha1.PhaseLoading
	case svc.Spec.Scaling.Min == 0:
		phase = servingv1alpha1.PhaseScaledToZero
	}

	svc.Status.Phase = phase
	svc.Status.ResolvedRuntime = runtimeName
	svc.Status.Replicas = deployment.Status.ReadyReplicas
	svc.Status.EndpointURL = fmt.Sprintf("http://%s.%s.svc/v1", svc.Name, svc.Namespace)

	meta.SetStatusCondition(&svc.Status.Conditions, metav1.Condition{
		Type:               conditionAutoscalingReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ScaledObjectApplied",
		Message:            "KEDA External Push autoscaling is configured",
		ObservedGeneration: svc.Generation,
	})

	ready := metav1.Condition{Type: conditionReady, ObservedGeneration: svc.Generation}
	switch phase {
	case servingv1alpha1.PhaseReady:
		ready.Status, ready.Reason, ready.Message = metav1.ConditionTrue, "Available", "Serving pods are ready"
	case servingv1alpha1.PhaseScaledToZero:
		ready.Status, ready.Reason, ready.Message = metav1.ConditionFalse, "ScaledToZero", "Backend is scaled to zero"
	case servingv1alpha1.PhaseLoading, servingv1alpha1.PhaseDegraded:
		ready.Status, ready.Reason, ready.Message = metav1.ConditionFalse, observation.reason, observation.message
	default:
		ready.Status, ready.Reason, ready.Message = metav1.ConditionFalse, "Pending", "Waiting for the backend Deployment"
	}
	meta.SetStatusCondition(&svc.Status.Conditions, ready)

	degraded := metav1.Condition{
		Type:               conditionDegraded,
		ObservedGeneration: svc.Generation,
	}
	switch {
	case observation.degraded:
		degraded.Status, degraded.Reason, degraded.Message =
			metav1.ConditionTrue, observation.reason, observation.message
	case phase == servingv1alpha1.PhaseScaledToZero:
		degraded.Status, degraded.Reason, degraded.Message =
			metav1.ConditionFalse, "Inactive", "Backend is scaled to zero and has no observed activation failure"
	case phase == servingv1alpha1.PhaseReady:
		degraded.Status, degraded.Reason, degraded.Message =
			metav1.ConditionFalse, "Healthy", "No backend activation failure is observed"
	default:
		degraded.Status, degraded.Reason, degraded.Message =
			metav1.ConditionFalse, "Progressing", "Backend activation is progressing without an observed failure"
	}
	meta.SetStatusCondition(&svc.Status.Conditions, degraded)

	if apiequality.Semantic.DeepEqual(oldStatus, &svc.Status) {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, svc); err != nil {
		return ctrl.Result{}, err
	}
	r.reportBackendTransition(ctx, svc, previousDegraded, &degraded)
	return ctrl.Result{}, nil
}

func (r *LLMServiceReconciler) reportBackendTransition(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
	previous *metav1.Condition,
	current *metav1.Condition,
) {
	log := logf.FromContext(ctx)
	if current.Status == metav1.ConditionTrue &&
		(previous == nil || previous.Status != metav1.ConditionTrue || previous.Reason != current.Reason) {
		log.Info(
			"Observed backend activation failure",
			"llmservice", svc.Name,
			"namespace", svc.Namespace,
			"failureClass", current.Reason,
		)
		if r.Recorder != nil {
			r.Recorder.Eventf(
				svc,
				nil,
				corev1.EventTypeWarning,
				current.Reason,
				"ObserveBackendFailure",
				"%s",
				current.Message,
			)
		}
		return
	}
	if previous != nil && previous.Status == metav1.ConditionTrue && current.Status != metav1.ConditionTrue {
		log.Info(
			"Observed backend activation recovery",
			"llmservice", svc.Name,
			"namespace", svc.Namespace,
			"previousFailureClass", previous.Reason,
		)
		if r.Recorder != nil {
			r.Recorder.Eventf(
				svc,
				nil,
				corev1.EventTypeNormal,
				"BackendRecovered",
				"ObserveBackendRecovery",
				"%s",
				"Backend activation recovered",
			)
		}
	}
}

func (r *LLMServiceReconciler) fail(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
	reason string,
	err error,
) (ctrl.Result, error) {
	return r.failWithAutoscaling(ctx, svc, reason, err, false)
}

func (r *LLMServiceReconciler) failAfterAutoscaling(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
	reason string,
	err error,
) (ctrl.Result, error) {
	return r.failWithAutoscaling(ctx, svc, reason, err, true)
}

func (r *LLMServiceReconciler) failWithAutoscaling(
	ctx context.Context,
	svc *servingv1alpha1.LLMService,
	reason string,
	err error,
	autoscalingApplied bool,
) (ctrl.Result, error) {
	logf.FromContext(ctx).Error(err, "Failed to reconcile LLMService", "reason", reason)
	oldStatus := svc.Status.DeepCopy()
	svc.Status.Phase = servingv1alpha1.PhaseDegraded
	autoscaling := metav1.Condition{
		Type:               conditionAutoscalingReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            err.Error(),
		ObservedGeneration: svc.Generation,
	}
	if autoscalingApplied {
		autoscaling.Status = metav1.ConditionTrue
		autoscaling.Reason = "ScaledObjectApplied"
		autoscaling.Message = "KEDA External Push autoscaling is configured"
	}
	meta.SetStatusCondition(&svc.Status.Conditions, autoscaling)
	meta.SetStatusCondition(&svc.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            err.Error(),
		ObservedGeneration: svc.Generation,
	})
	meta.SetStatusCondition(&svc.Status.Conditions, metav1.Condition{
		Type:               conditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            err.Error(),
		ObservedGeneration: svc.Generation,
	})
	if apiequality.Semantic.DeepEqual(oldStatus, &svc.Status) {
		return ctrl.Result{}, err
	}
	if updateErr := r.Status().Update(ctx, svc); updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}
