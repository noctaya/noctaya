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
	"time"

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
	"k8s.io/client-go/tools/events"
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

			managed := client.MatchingLabels{managedByLabel: managedByValue}
			var pvcs corev1.PersistentVolumeClaimList
			Expect(k8sClient.List(ctx, &pvcs, client.InNamespace(namespace), managed)).To(Succeed())
			for i := range pvcs.Items {
				pvcs.Items[i].Finalizers = nil
				Expect(k8sClient.Update(ctx, &pvcs.Items[i])).To(Succeed())
			}

			background := metav1.DeletePropagationBackground
			deleteOptions := []client.DeleteAllOfOption{
				client.InNamespace(namespace),
				managed,
				client.PropagationPolicy(background),
			}
			Expect(k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, deleteOptions...)).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Service{}, deleteOptions...)).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Pod{}, deleteOptions...)).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, deleteOptions...)).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.PersistentVolumeClaim{}, deleteOptions...)).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &batchv1.Job{}, deleteOptions...)).To(Succeed())
			foreignSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: svcName + "-demand-auth", Namespace: namespace,
			}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreignSecret))).To(Succeed())

			scaledObject := &unstructured.Unstructured{}
			scaledObject.SetAPIVersion("keda.sh/v1alpha1")
			scaledObject.SetKind("ScaledObject")
			Expect(k8sClient.DeleteAllOf(ctx, scaledObject, deleteOptions...)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, &servingv1alpha1.LLMService{})).To(Satisfy(apierrors.IsNotFound))
				g.Expect(k8sClient.Get(ctx, runtimeKey, &servingv1alpha1.InferenceRuntime{})).To(Satisfy(apierrors.IsNotFound))
				assertNoManagedObjects := func(list client.ObjectList) {
					g.Expect(k8sClient.List(ctx, list, client.InNamespace(namespace), managed)).To(Succeed())
					switch items := list.(type) {
					case *appsv1.DeploymentList:
						g.Expect(items.Items).To(BeEmpty())
					case *corev1.ServiceList:
						g.Expect(items.Items).To(BeEmpty())
					case *corev1.PodList:
						g.Expect(items.Items).To(BeEmpty())
					case *corev1.SecretList:
						g.Expect(items.Items).To(BeEmpty())
					case *corev1.PersistentVolumeClaimList:
						g.Expect(items.Items).To(BeEmpty())
					case *batchv1.JobList:
						g.Expect(items.Items).To(BeEmpty())
					}
				}
				assertNoManagedObjects(&appsv1.DeploymentList{})
				assertNoManagedObjects(&corev1.ServiceList{})
				assertNoManagedObjects(&corev1.PodList{})
				assertNoManagedObjects(&corev1.SecretList{})
				assertNoManagedObjects(&corev1.PersistentVolumeClaimList{})
				assertNoManagedObjects(&batchv1.JobList{})
				g.Expect(k8sClient.Get(
					ctx,
					types.NamespacedName{Name: svcName + "-demand-auth", Namespace: namespace},
					&corev1.Secret{},
				)).To(Satisfy(apierrors.IsNotFound))
			}).Should(Succeed())
		})

		It("renders a Deployment and Service from the selected runtime", func() {
			stored := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, stored)).To(Succeed())
			Expect(stored.Spec.Resources.Accelerators).To(Equal(int32(1)))
			Expect(stored.Spec.Scaling.Min).To(BeZero())
			Expect(stored.Spec.Scaling.Max).To(Equal(int32(1)))
			Expect(stored.Spec.Scaling.Metric).To(Equal("queueDepth"))
			Expect(stored.Spec.Scaling.Target).To(Equal(int32(10)))
			Expect(stored.Spec.Scaling.ActivationTimeout.Duration).To(Equal(5 * time.Minute))
			Expect(stored.Spec.Scaling.ScaleDownStabilization.Duration).To(Equal(5 * time.Minute))
			Expect(stored.Spec.Scaling.DrainTimeout.Duration).To(Equal(2 * time.Minute))
			Expect(stored.Spec.Cache.Strategy).To(Equal("NodeLocalPVC"))
			Expect(stored.Spec.Endpoint.OpenAICompatible).To(BeTrue())
			Expect(stored.Spec.Endpoint.ColdStart.Mode).To(Equal("keepalive"))
			Expect(stored.Spec.Endpoint.ColdStart.HeartbeatInterval.Duration).To(Equal(10 * time.Second))

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
			Expect(*gwDep.Spec.Replicas).To(Equal(int32(1)))
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

		It("reports an image-pull failure once and recovers without recreating the service", func() {
			r := reconciler()
			r.Recorder = events.NewFakeRecorder(4)
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
			deployment.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "qwen3-8b-failing",
					Namespace: namespace,
					Labels:    deployment.Spec.Template.Labels,
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "serving",
					Image: "invalid.example/noctaya/missing:test",
				}}},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status.Phase = corev1.PodPending
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
				Name:  "serving",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reasonImagePullBackOff}},
			}}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			failed := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, failed)).To(Succeed())
			Expect(failed.Status.Phase).To(Equal(servingv1alpha1.PhaseDegraded))
			degraded := meta.FindStatusCondition(failed.Status.Conditions, "Degraded")
			Expect(degraded).NotTo(BeNil())
			Expect(degraded.Status).To(Equal(metav1.ConditionTrue))
			Expect(degraded.Reason).To(Equal(reasonImagePullFailed))
			Expect(degraded.ObservedGeneration).To(Equal(failed.Generation))
			ready := meta.FindStatusCondition(failed.Status.Conditions, "Ready")
			Expect(ready.Reason).To(Equal(reasonImagePullFailed))

			var event string
			Eventually(r.Recorder.(*events.FakeRecorder).Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Warning ImagePullFailed"))

			failedResourceVersion := failed.ResourceVersion
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			unchanged := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, unchanged)).To(Succeed())
			Expect(unchanged.ResourceVersion).To(Equal(failedResourceVersion))
			Consistently(r.Recorder.(*events.FakeRecorder).Events).ShouldNot(Receive())

			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			readyPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "qwen3-8b-ready",
					Namespace: namespace,
					Labels:    deployment.Spec.Template.Labels,
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "serving",
					Image: "vllm/vllm-openai:v0.22.0",
				}}},
			}
			Expect(k8sClient.Create(ctx, readyPod)).To(Succeed())
			readyPod.Status.Phase = corev1.PodRunning
			readyPod.Status.ContainerStatuses = []corev1.ContainerStatus{{
				Name:  "serving",
				Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}
			Expect(k8sClient.Status().Update(ctx, readyPod)).To(Succeed())

			Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1
			deployment.Status.AvailableReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			recovered := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, recovered)).To(Succeed())
			Expect(recovered.Status.Phase).To(Equal(servingv1alpha1.PhaseReady))
			Expect(meta.FindStatusCondition(recovered.Status.Conditions, "Ready").Status).To(Equal(metav1.ConditionTrue))
			degraded = meta.FindStatusCondition(recovered.Status.Conditions, "Degraded")
			Expect(degraded.Status).To(Equal(metav1.ConditionFalse))
			Expect(degraded.Reason).To(Equal("Healthy"))
			Eventually(r.Recorder.(*events.FakeRecorder).Events).Should(Receive(ContainSubstring("Normal BackendRecovered")))
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
			demandSecret := &corev1.Secret{}
			demandSecretKey := types.NamespacedName{Name: svcName + "-demand-auth", Namespace: namespace}
			Expect(k8sClient.Get(ctx, demandSecretKey, demandSecret)).To(Succeed())
			Expect(demandSecret.Data["token"]).To(HaveLen(43))

			r.GatewayReplicas = 1
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, scalerKey, &appsv1.Deployment{})).To(Satisfy(apierrors.IsNotFound))
			Expect(k8sClient.Get(ctx, scalerKey, scalerService)).To(Succeed())
			Expect(scalerService.Spec.Selector).To(HaveKeyWithValue("serving.noctaya.io/gateway", svcName))
			Expect(k8sClient.Get(ctx, demandSecretKey, &corev1.Secret{})).To(Satisfy(apierrors.IsNotFound))
		})

		It("reports missing KEDA without creating a model backend", func() {
			r := reconciler()
			r.GatewayReplicas = 2
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
			Expect(k8sClient.Get(
				ctx,
				types.NamespacedName{Name: svcName + "-demand-auth", Namespace: namespace},
				&corev1.Secret{},
			)).To(Satisfy(apierrors.IsNotFound))
		})

		It("does not apply resources when desired-state rendering fails", func() {
			service := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, service)).To(Succeed())
			service.Spec.Cache.Strategy = "SharedPVC"
			Expect(k8sClient.Update(ctx, service)).To(Succeed())

			_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).To(HaveOccurred())

			scaledObject := &unstructured.Unstructured{}
			scaledObject.SetAPIVersion("keda.sh/v1alpha1")
			scaledObject.SetKind("ScaledObject")
			Expect(k8sClient.Get(ctx, key, scaledObject)).To(Satisfy(apierrors.IsNotFound))
			Expect(k8sClient.Get(ctx, key, &appsv1.Deployment{})).To(Satisfy(apierrors.IsNotFound))
		})

		It("marks autoscaling unavailable for generic ScaledObject apply failures", func() {
			r := reconciler()
			r.Client = &applyErrorClient{Client: k8sClient, err: errors.New("apply failed")}

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).To(MatchError("apply failed"))

			updated := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
			condition := meta.FindStatusCondition(updated.Status.Conditions, "AutoscalingReady")
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("ApplyScaledObject"))
			Expect(condition.ObservedGeneration).To(Equal(updated.Generation))
		})

		It("refuses an unowned demand authentication Secret", func() {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: svcName + "-demand-auth", Namespace: namespace,
			}}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			r := reconciler()
			r.GatewayReplicas = 2

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).To(MatchError(ContainSubstring("is not controlled")))
			Expect(k8sClient.Get(ctx, key, &appsv1.Deployment{})).To(Satisfy(apierrors.IsNotFound))
		})

		It("fails closed when an owned demand authentication Secret is malformed", func() {
			r := reconciler()
			r.GatewayReplicas = 2
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			secretKey := types.NamespacedName{Name: svcName + "-demand-auth", Namespace: namespace}
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
			secret.Data["token"] = []byte("short")
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).To(MatchError(ContainSubstring("must contain at least 32 characters")))

			updated := &servingv1alpha1.LLMService{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
			condition := meta.FindStatusCondition(updated.Status.Conditions, "AutoscalingReady")
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal("ScaledObjectApplied"))
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
