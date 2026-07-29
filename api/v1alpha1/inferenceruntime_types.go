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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// InferenceRuntimeSpec defines a reusable inference backend and its Kubernetes integration.
type InferenceRuntimeSpec struct {
	// family identifies the serving engine; v0 supports vLLM.
	// +kubebuilder:default=vllm
	// +kubebuilder:validation:Enum=vllm
	Family string `json:"family"`

	// vendor selects a registered Kubernetes backend adapter.
	// +kubebuilder:validation:Enum=nvidia;ascend
	Vendor string `json:"vendor"`

	// priority breaks ties when several runtimes match an LLMService selector (higher wins).
	// +kubebuilder:default=0
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// container must expose the OpenAI-compatible API on its configured port.
	Container RuntimeContainer `json:"container"`

	// accelerator describes the device-plugin resource and pod scheduling constraints.
	Accelerator AcceleratorSpec `json:"accelerator"`

	// health configures probes for model loading and serving.
	// +optional
	Health RuntimeHealth `json:"health,omitempty,omitzero"`

	// lifecycle configures graceful serving-pod termination.
	// +optional
	Lifecycle RuntimeLifecycle `json:"lifecycle,omitempty,omitzero"`

	// metrics describes optional runtime telemetry for external integrations.
	// Noctaya autoscaling uses the gateway queue and does not consume these fields.
	// +optional
	Metrics RuntimeMetrics `json:"metrics,omitempty,omitzero"`
}

// RuntimeContainer describes the serving container rendered into backend pods.
type RuntimeContainer struct {
	// image is the serving runtime image.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// args are Go-templated and rendered with model and service context.
	// +optional
	Args []string `json:"args,omitempty"`

	// env is copied to the serving container; literal values may use arg templates.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// port serves the OpenAI-compatible API and, when enabled by the runtime, metrics.
	Port RuntimePort `json:"port"`
}

// RuntimePort identifies the serving container's named TCP port.
type RuntimePort struct {
	// name is referenced by Services and probes.
	// +kubebuilder:default=http
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// containerPort is the serving API port.
	// +kubebuilder:default=8000
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ContainerPort int32 `json:"containerPort"`
}

// AcceleratorSpec maps an abstract request to a device-plugin resource and scheduler.
type AcceleratorSpec struct {
	// resourceName is the device-plugin resource key (e.g. nvidia.com/gpu,
	// huawei.com/Ascend910). Configurable because it varies by vendor and version.
	// +kubebuilder:validation:MinLength=1
	ResourceName string `json:"resourceName"`

	// sharing declares whether this runtime supports fractional devices (e.g. NVIDIA via HAMi).
	// +optional
	Sharing AcceleratorSharing `json:"sharing,omitempty,omitzero"`

	// nodeSelector constrains backend and prewarm pods to compatible nodes.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// tolerations let backend and prewarm pods use tainted accelerator nodes.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// scheduler routes pods to an existing scheduler (e.g. Volcano).
	// +optional
	Scheduler RuntimeScheduler `json:"scheduler,omitempty,omitzero"`
}

// AcceleratorSharing declares adapter support for fractional devices.
type AcceleratorSharing struct {
	// supported declares that the adapter can honor fractional device requests.
	// +optional
	Supported bool `json:"supported,omitempty"`
}

// RuntimeScheduler routes backend and prewarm pods to an installed scheduler.
type RuntimeScheduler struct {
	// name is the scheduler name; empty uses the default scheduler.
	// +optional
	Name string `json:"name,omitempty"`

	// queue is rendered as the scheduling.volcano.sh/queue-name pod annotation;
	// only meaningful together with name: volcano.
	// +optional
	Queue string `json:"queue,omitempty"`
}

// RuntimeHealth holds model-load-aware probes. startup gives slow weight loads
// minutes of headroom before liveness can kill the pod.
type RuntimeHealth struct {
	// readiness controls when the backend receives traffic.
	// +optional
	Readiness *corev1.Probe `json:"readiness,omitempty"`

	// liveness detects a failed serving process after startup.
	// +optional
	Liveness *corev1.Probe `json:"liveness,omitempty"`

	// startup protects slow model loading from liveness restarts.
	// +optional
	Startup *corev1.Probe `json:"startup,omitempty"`
}

// RuntimeLifecycle configures graceful termination of serving pods.
type RuntimeLifecycle struct {
	// terminationGracePeriodSeconds is the base pod shutdown budget; Noctaya widens
	// it when the configured drain timeout requires more time.
	// +kubebuilder:validation:Minimum=1
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`

	// preStopDrain stops routing new requests and waits for in-flight streams
	// to finish before the pod is torn down.
	// +optional
	PreStopDrain bool `json:"preStopDrain,omitempty"`
}

// RuntimeMetrics describes runtime metrics for external observability integrations.
type RuntimeMetrics struct {
	// path is the runtime's Prometheus scrape path.
	// +kubebuilder:default=/metrics
	Path string `json:"path"`

	// port names the Service port that exposes runtime metrics.
	// +kubebuilder:default=http
	Port string `json:"port"`

	// queueDepth is the runtime's pending-request metric.
	QueueDepth string `json:"queueDepth"`

	// kvCacheUtil is the runtime's KV-cache utilization metric.
	// +optional
	KVCacheUtil string `json:"kvCacheUtil,omitempty"`

	// running is the runtime's in-flight request metric.
	// +optional
	Running string `json:"running,omitempty"`

	// ttft is the runtime's time-to-first-token metric.
	// +optional
	TTFT string `json:"ttft,omitempty"`
}

// InferenceRuntimeStatus defines the observed state of InferenceRuntime.
type InferenceRuntimeStatus struct {
	// conditions describe the runtime's observed state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Family",type=string,JSONPath=".spec.family"
// +kubebuilder:printcolumn:name="Vendor",type=string,JSONPath=".spec.vendor"
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=".spec.container.image",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// InferenceRuntime is a cluster-scoped, reusable backend driver for one vendor runtime.
type InferenceRuntime struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec InferenceRuntimeSpec `json:"spec"`

	// +optional
	Status InferenceRuntimeStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// InferenceRuntimeList contains a list of InferenceRuntime
type InferenceRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []InferenceRuntime `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &InferenceRuntime{}, &InferenceRuntimeList{})
		return nil
	})
}
