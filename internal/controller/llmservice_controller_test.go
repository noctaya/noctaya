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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/backend/registry"
)

type applyErrorClient struct {
	client.Client
	err error
}

func (c *applyErrorClient) Apply(context.Context, k8sruntime.ApplyConfiguration, ...client.ApplyOption) error {
	return c.err
}

var _ = Describe("LLMService Controller", func() {
	Context("When reconciling an LLMService", func() {
		const (
			runtimeName = "vllm-nvidia"
			svcName     = "qwen3-8b"
			namespace   = "default"
		)

		ctx := context.Background()
		key := types.NamespacedName{Name: svcName, Namespace: namespace}
		runtimeKey := types.NamespacedName{Name: runtimeName}

		reconciler := func() *LLMServiceReconciler {
			return &LLMServiceReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				Backends:     registry.New(),
				GatewayImage: "ghcr.io/noctaya/noctaya-gateway:test",
			}
		}

		BeforeEach(func() {
			rt := &servingv1alpha1.InferenceRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: runtimeName},
				Spec: servingv1alpha1.InferenceRuntimeSpec{
					Family: "vllm",
					Vendor: "nvidia",
					Container: servingv1alpha1.RuntimeContainer{
						Image: "vllm/vllm-openai:v0.22.0",
						Args:  []string{"--model={{ .Model.Path }}", "--served-model-name={{ .Service.Name }}", "--port=8000"},
						Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
					},
					Accelerator: servingv1alpha1.AcceleratorSpec{ResourceName: "nvidia.com/gpu"},
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, rt))).To(Succeed())

			svc := &servingv1alpha1.LLMService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: namespace},
				Spec: servingv1alpha1.LLMServiceSpec{
					Model:   servingv1alpha1.ModelSpec{Source: &servingv1alpha1.ModelSource{URI: "modelscope://Qwen/Qwen3-8B-Instruct"}},
					Runtime: servingv1alpha1.RuntimeSelection{Name: runtimeName},
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, svc))).To(Succeed())
		})

		AfterEach(func() {
			svc := &servingv1alpha1.LLMService{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: namespace}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, svc))).To(Succeed())
			rt := &servingv1alpha1.InferenceRuntime{ObjectMeta: metav1.ObjectMeta{Name: runtimeName}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, rt))).To(Succeed())
			// owner GC does not run in envtest, so clean children explicitly
			Expect(k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(namespace))).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace(namespace))).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.PersistentVolumeClaim{}, client.InNamespace(namespace))).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &batchv1.Job{}, client.InNamespace(namespace))).To(Succeed())
		})

		It("renders a Deployment and Service from the selected runtime", func() {
			_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			c := dep.Spec.Template.Spec.Containers[0]
			Expect(c.Image).To(Equal("vllm/vllm-openai:v0.22.0"))
			Expect(c.Args).To(ContainElement("--model=Qwen/Qwen3-8B-Instruct"))
			Expect(c.Resources.Limits).To(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
			Expect(c.Env).To(ContainElement(corev1.EnvVar{Name: "VLLM_USE_MODELSCOPE", Value: "true"}))
			Expect(dep.OwnerReferences).NotTo(BeEmpty())

			backendSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: svcName + "-backend", Namespace: namespace}, backendSvc)).To(Succeed())
			Expect(backendSvc.Spec.Selector).To(HaveKeyWithValue("serving.noctaya.io/llmservice", svcName))

			gwDep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: svcName + "-gateway", Namespace: namespace}, gwDep)).To(Succeed())
			Expect(gwDep.Spec.Replicas).NotTo(BeNil())
			Expect(*gwDep.Spec.Replicas).To(Equal(int32(1))) // default: crisp scale-from-zero
			gwSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, key, gwSvc)).To(Succeed())
			Expect(gwSvc.Spec.Selector).To(HaveKeyWithValue("serving.noctaya.io/gateway", svcName))

			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: svcName + "-cache", Namespace: namespace}, pvc)).To(Succeed())

			updated := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
			Expect(updated.Status.ResolvedRuntime).To(Equal(runtimeName))
			Expect(updated.Status.EndpointURL).To(ContainSubstring(svcName))
			Expect(meta.FindStatusCondition(updated.Status.Conditions, "Ready").Reason).To(Equal("ScaledToZero"))
			Expect(meta.FindStatusCondition(updated.Status.Conditions, "AutoscalingReady").Status).To(Equal(metav1.ConditionTrue))

			scaledObject := &unstructured.Unstructured{}
			scaledObject.SetAPIVersion("keda.sh/v1alpha1")
			scaledObject.SetKind("ScaledObject")
			Expect(k8sClient.Get(ctx, key, scaledObject)).To(Succeed())
			triggers, found, err := unstructured.NestedSlice(scaledObject.Object, "spec", "triggers")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(triggers).To(HaveLen(1))
			Expect(triggers[0]).To(HaveKeyWithValue("type", "external-push"))
		})

		It("resolves the runtime via vendor selector", func() {
			decoy := &servingv1alpha1.InferenceRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: "vllm-ascend-decoy"},
				Spec: servingv1alpha1.InferenceRuntimeSpec{
					Family:   "vllm",
					Vendor:   "ascend",
					Priority: 100,
					Container: servingv1alpha1.RuntimeContainer{
						Image: "quay.io/ascend/vllm-ascend:v0.21.0rc1",
						Port:  servingv1alpha1.RuntimePort{Name: "http", ContainerPort: 8000},
					},
					Accelerator: servingv1alpha1.AcceleratorSpec{ResourceName: "huawei.com/Ascend910"},
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, decoy))).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, decoy))).To(Succeed())
			})

			selSvc := &servingv1alpha1.LLMService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName + "-sel", Namespace: namespace},
				Spec: servingv1alpha1.LLMServiceSpec{
					Model:   servingv1alpha1.ModelSpec{Source: &servingv1alpha1.ModelSource{URI: "modelscope://Qwen/Qwen3-8B-Instruct"}},
					Runtime: servingv1alpha1.RuntimeSelection{Selector: &servingv1alpha1.RuntimeSelector{Vendor: []string{"nvidia"}}},
				},
			}
			Expect(k8sClient.Create(ctx, selSvc)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, selSvc))).To(Succeed())
			})

			selKey := types.NamespacedName{Name: selSvc.Name, Namespace: namespace}
			_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: selKey})
			Expect(err).NotTo(HaveOccurred())

			updated := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, selKey, updated)).To(Succeed())
			Expect(updated.Status.ResolvedRuntime).To(Equal(runtimeName))
		})

		It("is idempotent across repeated reconciles", func() {
			_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		})

		It("renders the internal External Push scaler", func() {
			_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			gwDep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: svcName + "-gateway", Namespace: namespace}, gwDep)).To(Succeed())
			container := gwDep.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "NOCTAYA_SCALER_LISTEN_ADDR", Value: ":9090"}))

			scalerService := &corev1.Service{}
			scalerKey := types.NamespacedName{Name: svcName + "-scaler", Namespace: namespace}
			Expect(k8sClient.Get(ctx, scalerKey, scalerService)).To(Succeed())
			Expect(scalerService.Spec.Ports).To(HaveLen(1))
			Expect(scalerService.Spec.Ports[0].Name).To(Equal("grpc"))
		})

		It("aggregates demand for multiple gateway replicas", func() {
			r := reconciler()
			r.GatewayReplicas = 2
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			gatewayDeployment := &appsv1.Deployment{}
			gatewayKey := types.NamespacedName{Name: svcName + "-gateway", Namespace: namespace}
			Expect(k8sClient.Get(ctx, gatewayKey, gatewayDeployment)).To(Succeed())
			Expect(gatewayDeployment.Spec.Replicas).To(HaveValue(Equal(int32(2))))
			container := gatewayDeployment.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  "NOCTAYA_DEMAND_AGGREGATOR_URL",
				Value: "http://qwen3-8b-scaler.default.svc:9091/v1/demand",
			}))

			scalerKey := types.NamespacedName{Name: svcName + "-scaler", Namespace: namespace}
			scalerDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, scalerKey, scalerDeployment)).To(Succeed())
			Expect(scalerDeployment.Spec.Replicas).To(HaveValue(Equal(int32(1))))
			Expect(scalerDeployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"--mode=aggregator"}))

			scalerService := &corev1.Service{}
			Expect(k8sClient.Get(ctx, scalerKey, scalerService)).To(Succeed())
			Expect(scalerService.Spec.Selector).To(HaveKeyWithValue("serving.noctaya.io/scaler", svcName))
			Expect(scalerService.Spec.Ports).To(HaveLen(2))

			r.GatewayReplicas = 1
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, scalerKey, &appsv1.Deployment{})).To(Satisfy(apierrors.IsNotFound))
			Expect(k8sClient.Get(ctx, scalerKey, scalerService)).To(Succeed())
			Expect(scalerService.Spec.Selector).To(HaveKeyWithValue("serving.noctaya.io/gateway", svcName))
		})

		It("reports missing KEDA without creating a model backend", func() {
			r := reconciler()
			r.Client = &applyErrorClient{
				Client: k8sClient,
				err: &meta.NoKindMatchError{
					GroupKind:        schema.GroupKind{Group: "keda.sh", Kind: "ScaledObject"},
					SearchedVersions: []string{"v1alpha1"},
				},
			}

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).To(HaveOccurred())

			updated := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(servingv1alpha1.PhaseDegraded))
			condition := meta.FindStatusCondition(updated.Status.Conditions, "AutoscalingReady")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("AutoscalingUnavailable"))

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, key, deployment)).To(Satisfy(apierrors.IsNotFound))
		})

		It("marks the service Degraded when the runtime is missing", func() {
			rt := &servingv1alpha1.InferenceRuntime{ObjectMeta: metav1.ObjectMeta{Name: runtimeName}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, rt))).To(Succeed())
			Expect(k8sClient.Get(ctx, runtimeKey, &servingv1alpha1.InferenceRuntime{})).NotTo(Succeed())

			_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).To(HaveOccurred())
			_, err = reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).To(HaveOccurred(), "an unchanged failure status must not stop retries")

			updated := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(servingv1alpha1.PhaseDegraded))
		})
	})
})
