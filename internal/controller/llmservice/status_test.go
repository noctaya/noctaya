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
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

func TestStatusUpdateRetriesConflictWithoutOverwritingObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := servingv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	original := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", Generation: 3},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(original).WithObjects(original).Build()
	updates := 0
	wrapped := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(
			ctx context.Context,
			c client.Client,
			subresource string,
			obj client.Object,
			opts ...client.SubResourceUpdateOption,
		) error {
			updates++
			if updates == 1 {
				latest := &servingv1alpha1.LLMService{}
				if err := base.Get(ctx, client.ObjectKeyFromObject(obj), latest); err != nil {
					return err
				}
				latest.Annotations = map[string]string{"concurrent": "preserved"}
				latest.Spec.Scaling.Target = 7
				if err := base.Update(ctx, latest); err != nil {
					return err
				}
				return apierrors.NewConflict(
					schema.GroupResource{Group: servingv1alpha1.GroupVersion.Group, Resource: "llmservices"},
					obj.GetName(),
					errors.New("forced conflict"),
				)
			}
			return base.Status().Update(ctx, obj, opts...)
		},
	})
	desired := servingv1alpha1.LLMServiceStatus{
		Phase: servingv1alpha1.PhaseReady,
		Conditions: []metav1.Condition{{
			Type: "Ready", Status: metav1.ConditionTrue, Reason: "Available", ObservedGeneration: 3,
		}},
	}
	reconciler := &LLMServiceReconciler{Client: wrapped}
	previous, changed, err := reconciler.updateStatusWithRetry(ctx, original, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || updates != 2 {
		t.Fatalf("changed=%v updates=%d, want true and 2", changed, updates)
	}
	if previous == nil {
		t.Fatal("previous status was not captured")
	}

	stored := &servingv1alpha1.LLMService{}
	if err := base.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Annotations["concurrent"] != "preserved" || stored.Spec.Scaling.Target != 7 {
		t.Fatalf("concurrent object update was overwritten: %#v", stored)
	}
	if stored.Status.Phase != servingv1alpha1.PhaseReady || stored.Status.Conditions[0].ObservedGeneration != 3 {
		t.Fatalf("unexpected status: %#v", stored.Status)
	}
}

func TestStatusUpdateDoesNotRetryNonConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := servingv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	svc := &servingv1alpha1.LLMService{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(svc).WithObjects(svc).Build()
	want := errors.New("write denied")
	updates := 0
	wrapped := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(
			context.Context,
			client.Client,
			string,
			client.Object,
			...client.SubResourceUpdateOption,
		) error {
			updates++
			return want
		},
	})
	reconciler := &LLMServiceReconciler{Client: wrapped}
	_, _, err := reconciler.updateStatusWithRetry(ctx, svc, servingv1alpha1.LLMServiceStatus{Phase: servingv1alpha1.PhaseDegraded})
	if !errors.Is(err, want) || updates != 1 {
		t.Fatalf("err=%v updates=%d, want non-conflict error after one attempt", err, updates)
	}
}
