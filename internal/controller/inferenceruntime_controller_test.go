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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

var _ = Describe("InferenceRuntime Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		inferenceruntime := &servingv1alpha1.InferenceRuntime{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind InferenceRuntime")
			err := k8sClient.Get(ctx, typeNamespacedName, inferenceruntime)
			if err != nil && errors.IsNotFound(err) {
				resource := &servingv1alpha1.InferenceRuntime{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: servingv1alpha1.InferenceRuntimeSpec{
						Family: "vllm",
						Vendor: "nvidia",
						Container: servingv1alpha1.RuntimeContainer{
							Image: "vllm/vllm-openai:v0.22.0",
							Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
						},
						Accelerator: servingv1alpha1.AcceleratorSpec{ResourceName: "nvidia.com/gpu"},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &servingv1alpha1.InferenceRuntime{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance InferenceRuntime")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &InferenceRuntimeReconciler{
				Client: k8sClient,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
