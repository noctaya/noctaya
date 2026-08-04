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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	backendresources "github.com/noctaya/noctaya/internal/backend/resources"
)

const fieldOwner = client.FieldOwner("noctaya-operator")

// apply uses server-side apply for mutable owned resources.
func (r *LLMServiceReconciler) apply(
	ctx context.Context,
	owner *servingv1alpha1.LLMService,
	obj client.Object,
) error {
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}
	configuration := client.ApplyConfigurationFromUnstructured(&unstructured.Unstructured{Object: object})
	return r.Apply(ctx, configuration, fieldOwner, client.ForceOwnership)
}

// ensureCreated preserves immutable cache PVC and prewarm Job fields.
func (r *LLMServiceReconciler) ensureCreated(
	ctx context.Context,
	owner *servingv1alpha1.LLMService,
	obj client.Object,
) error {
	desiredHash := obj.GetAnnotations()[backendresources.CreateOnceHashAnnotation]
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	} else if apierrors.IsAlreadyExists(err) {
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		if !metav1.IsControlledBy(obj, owner) {
			return fmt.Errorf("resource %s/%s already exists and is not controlled by LLMService %s",
				obj.GetNamespace(), obj.GetName(), owner.Name)
		}
		if desiredHash != "" && obj.GetAnnotations()[backendresources.CreateOnceHashAnnotation] != desiredHash {
			return fmt.Errorf(
				"immutable resource %s/%s differs from the requested cache configuration; delete it explicitly before reconciling the change",
				obj.GetNamespace(), obj.GetName(),
			)
		}
	}
	return nil
}

func (r *LLMServiceReconciler) deleteOwned(
	ctx context.Context,
	owner *servingv1alpha1.LLMService,
	obj client.Object,
) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(obj, owner) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, obj))
}
