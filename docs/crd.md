# CRD reference

Noctaya CRDs use API group `serving.noctaya.io/v1alpha1`. This page documents the user-facing fields
from
[`api/v1alpha1/`](https://github.com/noctaya/noctaya/tree/main/api/v1alpha1)
and the generated CRD schemas. The API remains alpha: fields reserved for planned features may be
accepted by Kubernetes but rejected during reconciliation. Those fields are marked below; schema
presence alone is not a support claim.

## LLMService

`LLMService` is a namespaced resource that describes what model to serve, which runtime to use, how
many accelerator resources to request, how to scale, how to cache model weights, and how cold starts
should behave.

Only `spec.model` is required by the CRD schema. Other sections are optional and use the defaults
shown below when a default is defined.

For a service that can reconcile in the current implementation, set `spec.model.source.uri` and
select a runtime with either `spec.runtime.name` or `spec.runtime.selector.vendor`. Catalog-only
models and an omitted runtime are admitted by the alpha schema but reported as `Degraded`.

| Field | Type | Default / enum | Description |
|---|---|---|---|
| `spec.model` | object | required | Model identity and source. |
| `spec.model.catalogRef` | string | - | Reserved for model-catalog resolution and currently rejected. Use `spec.model.source.uri`. |
| `spec.model.source` | object | - | Inline model source configuration. |
| `spec.model.source.uri` | string | required when `source` is set | Model location. Supported schemes are `hf://` (with `huggingface://` as an alias), `modelscope://`, and `pvc://<claim>[/<subpath>]`. A `pvc://` source mounts pre-staged weights read-only at `/models`, performs no download, and should use `cache.strategy: None`. `oci://` and `s3://` are not implemented. |
| `spec.model.source.secretRef` | object | - | Reserved for private model sources; currently rejected during reconciliation. |
| `spec.model.source.secretRef.name` | string | `""` | Name of the Secret holding source credentials. |
| `spec.runtime` | object | - | Backend runtime selection. Pin a runtime by name or provide a vendor preference selector. |
| `spec.runtime.name` | string | - | Exact `InferenceRuntime` name to use, such as `vllm-nvidia-a100`. |
| `spec.runtime.selector` | object | - | Runtime auto-selection criteria. |
| `spec.runtime.selector.vendor` | string array | - | Acceptable vendors in preference order, for example `["nvidia", "ascend"]`. |
| `spec.runtime.argsOverride` | string array | - | Arguments appended after the selected runtime's templated arguments. A duplicate flag may override an earlier value when supported by the runtime CLI. |
| `spec.resources` | object | - | Abstract accelerator, CPU, and memory request mapped onto the selected runtime at reconcile time. |
| `spec.resources.accelerators` | integer | `1`, minimum `1` | Number of whole accelerator devices to request. |
| `spec.resources.fraction` | object | - | Reserved for sub-device sharing; currently rejected during reconciliation. |
| `spec.resources.fraction.memory` | quantity | - | Memory portion for a fractional accelerator request. |
| `spec.resources.fraction.cores` | integer | - | Core count for a fractional accelerator request. |
| `spec.resources.cpu` | quantity | - | CPU request for the serving workload. |
| `spec.resources.memory` | quantity | - | Equal memory request and limit for each backend replica. |
| `spec.scaling` | object | - | KEDA-driven autoscaling configuration. Noctaya supports LLM-aware signals rather than CPU or raw RPS. |
| `spec.scaling.min` | integer | `0`, minimum `0` | Minimum backend replicas. `0` enables scale-to-zero. |
| `spec.scaling.max` | integer | `1`, minimum `1` | Maximum backend replicas. |
| `spec.scaling.metric` | string | default `queueDepth`; enum `queueDepth`, `kvCacheUtil` | `queueDepth` drives scaling. `kvCacheUtil` is reserved and rejected during reconciliation. |
| `spec.scaling.target` | integer | `10`, minimum `1` | Desired metric value per replica. |
| `spec.scaling.activationTimeout` | duration string | `5m` | Cold-activation deadline. In `keepalive` mode it bounds how long a request waits for readiness; in `reject` mode it bounds the demand lease retained after the immediate `503`. |
| `spec.scaling.scaleDownStabilization` | duration string | `5m` | HPA stabilization window for scale-down and KEDA cooldown before the final transition to zero. Use whole seconds from `0s` through the HPA limit of `1h`. |
| `spec.scaling.drainTimeout` | duration string | `2m` | Pre-stop wait for in-flight requests when the selected runtime sets `spec.lifecycle.preStopDrain: true`. Noctaya widens the Pod termination grace period to this timeout plus a shutdown margin when needed. |
| `spec.cache` | object | - | Model-weight cache configuration for reducing cold-start downloads. |
| `spec.cache.strategy` | string | default `NodeLocalPVC`; enum `NodeLocalPVC`, `HostPath`, `SharedPVC`, `BakedImage`, `None` | Cache backend. `NodeLocalPVC`, `HostPath`, and `None` are implemented. `SharedPVC` and `BakedImage` are reserved and rejected during reconciliation. |
| `spec.cache.size` | quantity | `50Gi` (controller default) | Requested cache PVC size for `NodeLocalPVC`. |
| `spec.cache.storageClassName` | string | - | StorageClass for the cache PVC. Empty uses the cluster default. |
| `spec.cache.prewarm` | boolean | `false` | Creates a one-time Job that hydrates model weights for `NodeLocalPVC` or `HostPath`. It is skipped for `pvc://` sources because those weights are already staged. |
| `spec.endpoint` | object | - | Client-facing endpoint behavior. |
| `spec.endpoint.openAICompatible` | boolean | `true` | Informational in the current implementation; the gateway always exposes the OpenAI-compatible API. |
| `spec.endpoint.coldStart` | object | - | Behavior for requests received while the backend is scaled to zero or still loading. |
| `spec.endpoint.coldStart.mode` | string | default `keepalive`; enum `keepalive`, `reject` | `keepalive` holds streaming requests open with SSE heartbeats; `reject` returns fast `503 + Retry-After`. |
| `spec.endpoint.coldStart.heartbeatInterval` | duration string | `10s` | Interval between keepalive heartbeats while a cold streaming request is waiting. |
| `spec.imagePullSecrets` | object array | - | `LocalObjectReference`s applied to the backend, gateway, and prewarm pods so images from private / air-gapped registries can be pulled. The named Secrets must exist in the LLMService's namespace. |
| `spec.imagePullSecrets[].name` | string | `""` | Name of an image-pull Secret in the LLMService's namespace. |

Kubernetes quantity fields accept standard resource quantity strings such as `8`, `500m`, `32Gi`,
or `60Gi`. Duration fields use Go/Kubernetes duration strings such as `10s`, `2m`, or `5m`.

Cache PVCs and prewarm Jobs contain immutable fields and are created once. Changing the model or
cache settings does not rewrite an existing `<service>-prewarm` Job or `<service>-cache` PVC. Delete
the Job to rerun prewarming; replace a PVC only after preserving any data you need.

### LLMService status

| Field | Description |
|---|---|
| `status.phase` | Summary state: `Pending`, `Loading`, `Ready`, `ScaledToZero`, or `Degraded`. |
| `status.resolvedRuntime` | Name of the `InferenceRuntime` selected by the controller. |
| `status.replicas` | Number of ready backend replicas. Gateway replicas are not included. |
| `status.endpointURL` | In-cluster OpenAI-compatible base URL, ending in `/v1`. |
| `status.conditions` | Kubernetes conditions. Noctaya maintains a `Ready` condition with `ObservedGeneration` set to the reconciled generation. |

## InferenceRuntime

`InferenceRuntime` is cluster-scoped configuration consumed by the `LLMService` controller. Its own
controller is passive. Changing a runtime requeues services that pin it or select its vendor.

| Field | Type | Default / enum | Description |
|---|---|---|---|
| `spec.family` | string | required; default and only value `vllm` | Serving-engine family. |
| `spec.vendor` | string | required; enum `nvidia`, `ascend` | Registered backend adapter key. |
| `spec.priority` | integer | `0` | Tie-breaker within one vendor; higher values win. Vendor preference order wins before priority. Equal top priorities are rejected as ambiguous, so pin `spec.runtime.name` in that case. |
| `spec.container` | object | required | Serving-container definition. |
| `spec.container.image` | string | required | Image that exposes an OpenAI-compatible API. |
| `spec.container.args` | string array | - | Go templates rendered with `.Model.Path`, `.Service.Name`, and `.Service.Namespace`. `LLMService.spec.runtime.argsOverride` values are appended. |
| `spec.container.env` | Kubernetes `EnvVar` array | - | Environment copied to the container. Literal `value` strings may use the same templates; normal `valueFrom` sources remain available. |
| `spec.container.port` | object | required | Named TCP port for the serving API and, when the runtime exposes them there, metrics. |
| `spec.container.port.name` | string | `http` | Port name referenced by Services and probes. |
| `spec.container.port.containerPort` | integer | `8000`; range `1..65535` | Serving-container port number. |
| `spec.accelerator` | object | required | Device-plugin resource and Pod scheduling constraints. |
| `spec.accelerator.resourceName` | string | required | Extended resource advertised by the installed device plugin, such as `nvidia.com/gpu` or `huawei.com/Ascend910`. |
| `spec.accelerator.sharing` | object | - | Reserved fractional-device capability metadata. No current adapter implements fractional allocation. |
| `spec.accelerator.sharing.supported` | boolean | `false` | Declares runtime capability only; setting it to `true` does not enable sharing, and `LLMService.spec.resources.fraction` is currently rejected. |
| `spec.accelerator.nodeSelector` | string map | - | Node labels copied to backend and prewarm Pods. |
| `spec.accelerator.tolerations` | Kubernetes `Toleration` array | - | Tolerations copied to backend and prewarm Pods. |
| `spec.accelerator.scheduler` | object | - | Routes backend and prewarm Pods through an already-installed scheduler. |
| `spec.accelerator.scheduler.name` | string | empty (default scheduler) | Pod `schedulerName`, for example `volcano`. |
| `spec.accelerator.scheduler.queue` | string | - | Volcano queue rendered as the `scheduling.volcano.sh/queue-name` Pod annotation. A non-empty queue is rejected unless `scheduler.name` is `volcano`. |
| `spec.health` | object | - | Model-load-aware serving probes. |
| `spec.health.readiness` | Kubernetes `Probe` | controller default: HTTP `GET /health` | Controls when the backend receives traffic. |
| `spec.health.liveness` | Kubernetes `Probe` | none | Detects a failed process after startup. |
| `spec.health.startup` | Kubernetes `Probe` | controller default: HTTP `GET /health`, about 10 minutes | Protects slow model loading from liveness restarts. |
| `spec.lifecycle` | object | - | Graceful serving-Pod termination settings. |
| `spec.lifecycle.terminationGracePeriodSeconds` | integer | Kubernetes default; minimum `1` when set | Base Pod shutdown budget. Noctaya widens it to cover `drainTimeout` plus 10 seconds when draining is enabled. |
| `spec.lifecycle.preStopDrain` | boolean | `false` | Adds a pre-stop wait for `LLMService.spec.scaling.drainTimeout`. The serving image must contain `/bin/sh`. |
| `spec.metrics` | object | - | Runtime metric metadata for external integrations. Noctaya autoscaling uses gateway demand and does not consume these names. |
| `spec.metrics.path` | string | `/metrics` | Runtime Prometheus scrape path. |
| `spec.metrics.port` | string | `http` | Service port that exposes runtime metrics. |
| `spec.metrics.queueDepth` | string | required when `metrics` is set | Runtime pending-request metric name. |
| `spec.metrics.kvCacheUtil` | string | - | Runtime KV-cache-utilization metric name. |
| `spec.metrics.running` | string | - | Runtime in-flight-request metric name. |
| `spec.metrics.ttft` | string | - | Runtime time-to-first-token metric name. |

Runtime definitions describe Kubernetes integration; they do not install drivers, device plugins,
CANN, schedulers, or inference kernels. Because the `InferenceRuntime` controller is passive, its
`status.conditions` field is not currently populated; operational state is reported on each
`LLMService` that consumes the runtime. Planned unsupported directions are summarized in the
[roadmap](../ROADMAP.md).
