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

// Package llmservice reconciles the per-model serving lifecycle.
package llmservice

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend/registry"
	"github.com/noctaya/noctaya/internal/model"
)

const (
	managedByLabel   = "app.kubernetes.io/managed-by"
	managedByValue   = "noctaya"
	llmServiceLabel  = "serving.noctaya.io/llmservice"
	runtimeLabel     = "serving.noctaya.io/runtime"
	scaledObjectKind = "ScaledObject"
)

type LLMServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Backends *registry.Registry
	// GatewayImage is the data-plane proxy image the operator deploys per LLMService.
	GatewayImage string
	// GatewayReplicas is the per-LLMService gateway replica count.
	GatewayReplicas int32
	Recorder        events.EventRecorder
}

// +kubebuilder:rbac:groups=serving.noctaya.io,resources=llmservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=serving.noctaya.io,resources=llmservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=serving.noctaya.io,resources=inferenceruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=services;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keda.sh,resources=scaledobjects,verbs=get;list;watch;create;update;patch;delete

func (r *LLMServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var svc servingv1alpha1.LLMService
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rt, err := r.resolveRuntime(ctx, &svc)
	if err != nil {
		return r.fail(ctx, &svc, "RuntimeResolution", err)
	}

	adapter, ok := r.Backends.Get(rt.Spec.Vendor)
	if !ok {
		return r.fail(ctx, &svc, "UnsupportedVendor", fmt.Errorf("no backend adapter registered for vendor %q", rt.Spec.Vendor))
	}

	resolved, err := model.Resolve(svc.Spec.Model)
	if err != nil {
		return r.fail(ctx, &svc, "ModelResolution", err)
	}

	desired, reason, err := r.renderDesired(&svc, rt, adapter, resolved)
	if err != nil {
		return r.fail(ctx, &svc, reason, err)
	}
	reason, autoscalingApplied, err := r.applyDesired(ctx, &svc, desired)
	if err != nil {
		if autoscalingApplied {
			return r.failAfterAutoscaling(ctx, &svc, reason, err)
		}
		return r.fail(ctx, &svc, reason, err)
	}

	var live appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired.backendDeployment), &live); err != nil {
		return r.failAfterAutoscaling(ctx, &svc, "ObserveBackendDeployment", err)
	}

	var pods corev1.PodList
	if err := r.List(
		ctx,
		&pods,
		client.InNamespace(svc.Namespace),
		client.MatchingLabels{
			managedByLabel:  managedByValue,
			llmServiceLabel: svc.Name,
			runtimeLabel:    rt.Name,
		},
	); err != nil {
		return r.failAfterAutoscaling(ctx, &svc, "ObserveBackendPods", err)
	}

	log.Info("Reconciled LLMService", "runtime", rt.Name, "readyReplicas", live.Status.ReadyReplicas)
	return r.updateStatus(ctx, &svc, rt.Name, &live, pods.Items)
}

func (r *LLMServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Backends == nil {
		r.Backends = registry.New()
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorder("llmservice-controller")
	}
	scaledObject := &unstructured.Unstructured{}
	scaledObject.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "keda.sh", Version: "v1alpha1", Kind: scaledObjectKind,
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&servingv1alpha1.LLMService{}).
		Watches(&servingv1alpha1.InferenceRuntime{}, handler.EnqueueRequestsFromMapFunc(r.servicesForRuntime)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.serviceForBackendPod)).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Secret{}).
		Owns(&batchv1.Job{}).
		Owns(scaledObject, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("llmservice").
		Complete(r)
}
