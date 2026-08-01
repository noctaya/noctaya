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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// LLMServiceSpec describes a model, runtime, resources, scaling, caching, and endpoint.
type LLMServiceSpec struct {
	// model identifies the weights served by the backend.
	Model ModelSpec `json:"model"`

	// runtime selects the backend — pinned by name or auto-picked by vendor preference.
	Runtime RuntimeSelection `json:"runtime"`

	// resources requests accelerators and compute for each backend replica.
	// +kubebuilder:default={}
	// +optional
	Resources ResourceSpec `json:"resources,omitempty,omitzero"`

	// scaling configures KEDA and cold-start timing.
	// +kubebuilder:default={}
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty,omitzero"`

	// cache configures model-weight storage and prewarming.
	// +kubebuilder:default={}
	// +optional
	Cache CacheSpec `json:"cache,omitempty,omitzero"`

	// endpoint configures client-facing cold-start behavior.
	// +kubebuilder:default={}
	// +optional
	Endpoint EndpointSpec `json:"endpoint,omitempty,omitzero"`

	// imagePullSecrets are applied to the backend, gateway, and prewarm pods so images
	// from private/air-gapped registries can be pulled. The secrets must exist in the
	// LLMService's namespace.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// ModelSpec identifies an inline model source or a future catalog entry.
// +kubebuilder:validation:XValidation:rule="has(self.source) != (has(self.catalogRef) && size(self.catalogRef) > 0)",message="set exactly one of source or catalogRef"
type ModelSpec struct {
	// catalogRef points to an external model catalog entry. Catalog resolution
	// is currently not implemented in v0; use model.source.uri for now.
	// +optional
	CatalogRef string `json:"catalogRef,omitempty"`

	// source is the inline model location used in v0.
	// +optional
	Source *ModelSource `json:"source,omitempty"`
}

// ModelSource describes an inline model location and optional credentials.
type ModelSource struct {
	// uri is the model location. Supported in v0: hf://, modelscope://, and
	// pvc://<claim>[/<subpath>] (pre-staged weights on an existing PVC, mounted
	// read-only). oci:// and s3:// are not yet implemented.
	// +kubebuilder:validation:MinLength=1
	URI string `json:"uri"`

	// secretRef holds credentials for private sources (e.g. a ModelScope token).
	// Not supported in v0: setting it fails reconcile until private sources land.
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// RuntimeSelection pins a runtime or selects one by vendor preference.
// +kubebuilder:validation:XValidation:rule="(has(self.name) && size(self.name) > 0) != (has(self.selector) && has(self.selector.vendor) && size(self.selector.vendor) > 0)",message="set exactly one of name or selector.vendor"
type RuntimeSelection struct {
	// name pins one cluster-scoped InferenceRuntime.
	// +optional
	Name string `json:"name,omitempty"`

	// selector auto-picks among runtimes by vendor preference order.
	// +optional
	Selector *RuntimeSelector `json:"selector,omitempty"`

	// argsOverride is appended after the runtime's serving args. Duplicate flags may
	// override earlier values when supported by the runtime CLI.
	// +optional
	ArgsOverride []string `json:"argsOverride,omitempty"`
}

// RuntimeSelector defines ordered runtime preferences.
type RuntimeSelector struct {
	// vendor lists acceptable vendors in preference order.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Enum=nvidia;ascend
	// +optional
	Vendor []string `json:"vendor,omitempty"`
}

// ResourceSpec is the abstract accelerator request; it maps onto the runtime's
// accelerator definition at reconcile time.
type ResourceSpec struct {
	// accelerators is the number of whole devices requested per backend replica.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Accelerators int32 `json:"accelerators,omitempty"`

	// fraction requests a sub-device slice; valid only when the runtime supports sharing.
	// Not supported in v0 (no runtime supports sharing yet): setting it fails reconcile.
	// +optional
	Fraction *AcceleratorFraction `json:"fraction,omitempty"`

	// cpu is the CPU request for each backend replica.
	// +optional
	CPU *resource.Quantity `json:"cpu,omitempty"`

	// memory is the memory request and limit for each backend replica.
	// +optional
	Memory *resource.Quantity `json:"memory,omitempty"`
}

// AcceleratorFraction requests a fraction of a device (e.g. NVIDIA via HAMi).
type AcceleratorFraction struct {
	// memory requests a device-memory slice.
	// +optional
	Memory *resource.Quantity `json:"memory,omitempty"`

	// cores requests a device-compute share.
	// +optional
	Cores int32 `json:"cores,omitempty"`
}

// ScalingSpec configures KEDA-driven autoscaling. Scaling is intentionally limited
// to LLM-aware signals: CPU and raw RPS are not supported.
// +kubebuilder:validation:XValidation:rule="!has(self.min) || !has(self.max) || self.min <= self.max",message="min must not exceed max"
// +kubebuilder:validation:XValidation:rule="!has(self.activationTimeout) || duration(self.activationTimeout) > duration('0s')",message="activationTimeout must be greater than zero"
// +kubebuilder:validation:XValidation:rule="!has(self.drainTimeout) || duration(self.drainTimeout) >= duration('0s')",message="drainTimeout must not be negative"
type ScalingSpec struct {
	// min replicas; 0 enables scale-to-zero.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +optional
	Min int32 `json:"min,omitempty"`

	// max is the maximum number of backend replicas.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Max int32 `json:"max,omitempty"`

	// metric selects the scaling signal. Only queueDepth is wired to the autoscaler
	// in v0; kvCacheUtil is reserved and fails reconcile if selected.
	// +kubebuilder:validation:Enum=queueDepth;kvCacheUtil
	// +kubebuilder:default=queueDepth
	// +optional
	Metric string `json:"metric,omitempty"`

	// target is the desired metric value per replica.
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +optional
	Target int32 `json:"target,omitempty"`

	// activationTimeout is how long the gateway buffers a request during cold start.
	// +kubebuilder:default="5m"
	// +optional
	ActivationTimeout metav1.Duration `json:"activationTimeout,omitempty,omitzero"`

	// scaleDownStabilization delays HPA scale-down and the final transition to zero.
	// Values must use whole seconds and cannot exceed the HPA limit of one hour.
	// +kubebuilder:default="5m"
	// +optional
	ScaleDownStabilization metav1.Duration `json:"scaleDownStabilization,omitempty,omitzero"`

	// drainTimeout is the pre-stop wait for in-flight requests. Noctaya widens the
	// pod termination grace period when this timeout needs more room.
	// +kubebuilder:default="2m"
	// +optional
	DrainTimeout metav1.Duration `json:"drainTimeout,omitempty,omitzero"`

	// externalScaler configures transport security for KEDA's External Push connection.
	// +optional
	ExternalScaler *ExternalScalerSpec `json:"externalScaler,omitempty"`
}

// ExternalScalerSpec configures the KEDA External Push endpoint.
type ExternalScalerSpec struct {
	// tls enables mutual TLS between KEDA and the ExternalScaler.
	// +optional
	TLS *ExternalScalerTLSSpec `json:"tls,omitempty"`
}

// ExternalScalerTLSSpec references credentials managed outside Noctaya.
type ExternalScalerTLSSpec struct {
	// serverSecretName is a Secret in the LLMService namespace containing tls.crt,
	// tls.key, and ca.crt. The CA must verify KEDA's client certificate.
	// +kubebuilder:validation:MinLength=1
	ServerSecretName string `json:"serverSecretName"`

	// authenticationRef selects the KEDA authentication object that provides
	// caCert, tlsClientCert, and tlsClientKey.
	AuthenticationRef KEDAAuthenticationReference `json:"authenticationRef"`
}

// KEDAAuthenticationReference identifies credentials used by KEDA.
type KEDAAuthenticationReference struct {
	// name is the TriggerAuthentication or ClusterTriggerAuthentication name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// kind selects a namespaced or cluster-scoped KEDA authentication object.
	// +kubebuilder:validation:Enum=TriggerAuthentication;ClusterTriggerAuthentication
	// +kubebuilder:default=TriggerAuthentication
	// +optional
	Kind string `json:"kind,omitempty"`
}

// CacheSpec selects how model weights are cached so cold pods load from local disk
// instead of re-downloading on every scale-from-zero.
type CacheSpec struct {
	// strategy selects the model cache backend.
	// +kubebuilder:validation:Enum=NodeLocalPVC;HostPath;SharedPVC;BakedImage;None
	// +kubebuilder:default=NodeLocalPVC
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// size is the requested NodeLocalPVC capacity.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`

	// storageClassName pins the cache PVC to a StorageClass; empty uses the cluster
	// default. Set this on clusters without a default StorageClass (common on managed
	// or domestic clusters) or to target a specific disk type.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// prewarm requests a one-time Job that hydrates weights into the cache.
	// +optional
	Prewarm bool `json:"prewarm,omitempty"`
}

// EndpointSpec configures client-facing protocol and cold-start behavior.
type EndpointSpec struct {
	// openAICompatible is informational in v0: the gateway always serves the
	// OpenAI-compatible API. Reserved for future protocol selection.
	// +kubebuilder:default=true
	// +optional
	OpenAICompatible bool `json:"openAICompatible,omitempty"`

	// maxQueue bounds requests admitted by each gateway replica.
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxQueue int32 `json:"maxQueue,omitempty"`

	// resources configures CPU and memory for each gateway replica.
	// +optional
	Resources GatewayResourceSpec `json:"resources,omitempty,omitzero"`

	// authentication enables client-facing Bearer authentication.
	// +optional
	Authentication *EndpointAuthenticationSpec `json:"authentication,omitempty"`

	// coldStart configures requests received while the backend is unavailable.
	// +kubebuilder:default={}
	// +optional
	ColdStart ColdStartSpec `json:"coldStart,omitempty,omitzero"`
}

// GatewayResourceSpec configures standard compute requests and limits.
type GatewayResourceSpec struct {
	// requests reserves CPU and memory for each gateway replica.
	// +optional
	Requests GatewayComputeSpec `json:"requests,omitempty,omitzero"`

	// limits caps CPU and memory for each gateway replica.
	// +optional
	Limits GatewayComputeSpec `json:"limits,omitempty,omitzero"`
}

// GatewayComputeSpec contains the compute resources used by a gateway.
type GatewayComputeSpec struct {
	// cpu is a Kubernetes CPU quantity.
	// +optional
	CPU *resource.Quantity `json:"cpu,omitempty"`

	// memory is a Kubernetes memory quantity.
	// +optional
	Memory *resource.Quantity `json:"memory,omitempty"`
}

// EndpointAuthenticationSpec configures client-facing API-key authentication.
type EndpointAuthenticationSpec struct {
	// secretRef identifies the Secret key containing the accepted Bearer token.
	SecretRef SecretKeyReference `json:"secretRef"`
}

// SecretKeyReference identifies one key in a Secret in the LLMService namespace.
type SecretKeyReference struct {
	// name is the Secret name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// key is the data key containing the API key.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// ColdStartSpec configures how the gateway handles a cold backend.
// +kubebuilder:validation:XValidation:rule="!has(self.heartbeatInterval) || duration(self.heartbeatInterval) > duration('0s')",message="heartbeatInterval must be greater than zero"
type ColdStartSpec struct {
	// mode is how cold requests are handled: keepalive (SSE heartbeats hold the
	// connection) or reject (fast 503 + Retry-After for the client to retry).
	// +kubebuilder:validation:Enum=keepalive;reject
	// +kubebuilder:default=keepalive
	// +optional
	Mode string `json:"mode,omitempty"`

	// heartbeatInterval controls SSE keepalive comments while activation waits.
	// +kubebuilder:default="10s"
	// +optional
	HeartbeatInterval metav1.Duration `json:"heartbeatInterval,omitempty,omitzero"`
}

// LLMServicePhase summarizes backend availability.
// +kubebuilder:validation:Enum=Pending;Loading;Ready;ScaledToZero;Degraded
type LLMServicePhase string

const (
	PhasePending      LLMServicePhase = "Pending"
	PhaseLoading      LLMServicePhase = "Loading"
	PhaseReady        LLMServicePhase = "Ready"
	PhaseScaledToZero LLMServicePhase = "ScaledToZero"
	PhaseDegraded     LLMServicePhase = "Degraded"
)

// LLMServiceStatus defines the observed state of LLMService.
type LLMServiceStatus struct {
	// phase summarizes backend availability.
	// +optional
	Phase LLMServicePhase `json:"phase,omitempty"`

	// resolvedRuntime is the InferenceRuntime actually selected.
	// +optional
	ResolvedRuntime string `json:"resolvedRuntime,omitempty"`

	// replicas is the number of ready backend replicas.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// endpointURL is the OpenAI-compatible base URL clients should call.
	// +optional
	EndpointURL string `json:"endpointURL,omitempty"`

	// conditions describe the service's observed state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=".status.resolvedRuntime"
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// LLMService declaratively serves one model behind a scale-to-zero gateway.
type LLMService struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec LLMServiceSpec `json:"spec"`

	// +optional
	Status LLMServiceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LLMServiceList contains a list of LLMService
type LLMServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LLMService `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &LLMService{}, &LLMServiceList{})
		return nil
	})
}
