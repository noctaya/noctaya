# CRD reference

Noctaya exposes two resources in `serving.noctaya.io/v1alpha1`.

| Resource | Scope | Purpose |
|---|---|---|
| `LLMService` | Namespaced | Declares a model endpoint and its resources, scaling, cache, and cold-start behavior |
| `InferenceRuntime` | Cluster | Defines a reusable serving image and its accelerator, scheduling, health, and lifecycle integration |

The API is alpha. Kubernetes may accept reserved fields that the controller cannot yet reconcile; those fields are marked **not implemented** below. The Go types in [`api/v1alpha1`](https://github.com/noctaya/noctaya/tree/main/api/v1alpha1) and the generated CRDs remain authoritative.

Durations use Go/Kubernetes syntax such as `10s`, `2m`, and `5m`. Resource quantities use Kubernetes syntax such as `500m`, `32Gi`, and `60Gi`.

## LLMService

The schema requires `spec.model`. A working service also needs `spec.model.source.uri` and either `spec.runtime.name` or `spec.runtime.selector.vendor`; otherwise reconciliation reports `Degraded`.

### Model and runtime

| Field | Type | Default / constraint | Behavior |
|---|---|---|---|
| `spec.model.source.uri` | string | Required for reconciliation | Supports `hf://` (`huggingface://` alias), `modelscope://`, and `pvc://<claim>[/<subpath>]` |
| `spec.model.catalogRef` | string | **Not implemented** | Reserved for model-catalog lookup |
| `spec.model.source.secretRef` | `LocalObjectReference` | **Not implemented** | Reserved for private-source credentials |
| `spec.runtime.name` | string | Optional | Pins one cluster-scoped `InferenceRuntime`; takes precedence over a selector |
| `spec.runtime.selector.vendor` | string array | Optional | Selects the first available vendor in order, then the highest-priority runtime |
| `spec.runtime.argsOverride` | string array | Optional | Appends arguments after the runtime's templated arguments |

A `pvc://` source mounts existing weights read-only at `/models` and performs no download. Use `cache.strategy: None` to avoid creating a separate cache PVC. Other URI schemes, including `oci://` and `s3://`, are not implemented.

### Resources and scaling

| Field | Type | Default / constraint | Behavior |
|---|---|---|---|
| `spec.resources.accelerators` | integer | `1`; minimum `1` | Whole devices requested per backend replica |
| `spec.resources.cpu` | quantity | Optional | CPU request per backend replica |
| `spec.resources.memory` | quantity | Optional | Equal memory request and limit per backend replica |
| `spec.resources.fraction` | object | **Not implemented** | Reserved for sub-device sharing |
| `spec.scaling.min` | integer | `0`; minimum `0` | Minimum backend replicas; `0` enables scale-to-zero |
| `spec.scaling.max` | integer | `1`; minimum `1` | Maximum backend replicas; must be at least `min` |
| `spec.scaling.metric` | string | `queueDepth`; `queueDepth` or `kvCacheUtil` | Only `queueDepth` is implemented |
| `spec.scaling.target` | integer | `10`; minimum `1` | Queue-depth target per backend replica |
| `spec.scaling.activationTimeout` | duration | `5m` | Bounds a cold request wait or reject-mode activation lease |
| `spec.scaling.scaleDownStabilization` | duration | `5m`; whole seconds, `0s..1h` | HPA stabilization and KEDA cooldown before scale-to-zero |
| `spec.scaling.drainTimeout` | duration | `2m` | Pre-stop drain time when the runtime enables `preStopDrain` |

KEDA owns backend replicas; `min` and `max` do not control gateway replicas.

### Cache and endpoint

| Field | Type | Default / constraint | Behavior |
|---|---|---|---|
| `spec.cache.strategy` | string | `NodeLocalPVC` | Implements `NodeLocalPVC`, `HostPath`, and `None`; `SharedPVC` and `BakedImage` are not implemented |
| `spec.cache.size` | quantity | `50Gi` controller default | PVC request for `NodeLocalPVC` |
| `spec.cache.storageClassName` | string | Cluster default | Selects the cache PVC StorageClass |
| `spec.cache.prewarm` | boolean | `false` | Creates one download Job for `hf://` or `modelscope://` with a persistent cache |
| `spec.endpoint.openAICompatible` | boolean | `true` | Informational; the gateway currently always serves the OpenAI-compatible API |
| `spec.endpoint.coldStart.mode` | string | `keepalive`; `keepalive` or `reject` | Holds streaming requests with SSE heartbeats or returns `503` with `Retry-After` |
| `spec.endpoint.coldStart.heartbeatInterval` | duration | `10s` | Keepalive heartbeat interval during activation |
| `spec.imagePullSecrets` | `LocalObjectReference` array | Optional | Applied to backend, gateway, and prewarm Pods; Secrets must be in the service namespace |

Cache PVCs and prewarm Jobs are create-once resources. Delete the Job to prewarm again; preserve needed data before replacing a PVC.

### LLMService status

| Field | Description |
|---|---|
| `status.phase` | Summary state: `Pending`, `Loading`, `Ready`, `ScaledToZero`, or `Degraded`. |
| `status.resolvedRuntime` | Name of the `InferenceRuntime` selected by the controller. |
| `status.replicas` | Number of ready backend replicas. Gateway replicas are not included. |
| `status.endpointURL` | In-cluster OpenAI-compatible base URL, ending in `/v1`. |
| `status.conditions` | Kubernetes conditions. Noctaya maintains a `Ready` condition with `ObservedGeneration` set to the reconciled generation. |

## InferenceRuntime

`InferenceRuntime` is reusable configuration, not a workload. Its controller is passive; the `LLMService` controller consumes it and requeues matching services when it changes.

### Runtime and container

| Field | Type | Default / constraint | Behavior |
|---|---|---|---|
| `spec.family` | string | `vllm`; only `vllm` | Serving-engine family |
| `spec.vendor` | string | Required; `nvidia` or `ascend` | Selects the registered backend adapter |
| `spec.priority` | integer | `0` | Higher value wins within a selected vendor; equal top values are ambiguous |
| `spec.container.image` | string | Required | Serving image exposing an OpenAI-compatible API |
| `spec.container.args` | string array | Optional | Templates using `.Model.Path`, `.Service.Name`, and `.Service.Namespace` |
| `spec.container.env` | Kubernetes `EnvVar` array | Optional | Copied to the container; literal values support the same templates |
| `spec.container.port.name` | string | `http` | Port name used by Services and probes |
| `spec.container.port.containerPort` | integer | `8000`; `1..65535` | Serving API port |

`spec.container` and `spec.container.port` are required objects. Runtime arguments are rendered first; `LLMService.spec.runtime.argsOverride` is appended.

### Accelerator and scheduling

| Field | Type | Default / constraint | Behavior |
|---|---|---|---|
| `spec.accelerator.resourceName` | string | Required | Device-plugin resource, for example `nvidia.com/gpu` |
| `spec.accelerator.sharing.supported` | boolean | `false`; **not implemented** | Capability placeholder; it does not enable fractional allocation |
| `spec.accelerator.nodeSelector` | string map | Optional | Copied to backend and prewarm Pods |
| `spec.accelerator.tolerations` | Kubernetes `Toleration` array | Optional | Copied to backend and prewarm Pods |
| `spec.accelerator.scheduler.name` | string | Kubernetes default scheduler | Sets Pod `schedulerName`, for example `volcano` |
| `spec.accelerator.scheduler.queue` | string | Requires `scheduler.name: volcano` | Adds the Volcano queue annotation |

The runtime references existing drivers, device plugins, and schedulers; it does not install or manage them.

### Health, lifecycle, and metrics

| Field | Default / constraint | Behavior |
|---|---|---|
| `spec.health.readiness` | Controller default: HTTP `GET /health` | Gates backend traffic until the model is loaded |
| `spec.health.startup` | Controller default: `GET /health`, 10-second period, 60 failures | Allows about 10 minutes for startup |
| `spec.health.liveness` | None | Optional post-start failure detection |
| `spec.lifecycle.terminationGracePeriodSeconds` | Optional; minimum `1` | Base Pod shutdown budget |
| `spec.lifecycle.preStopDrain` | `false` | Sleeps for the service `drainTimeout`; requires `/bin/sh` in the image |
| `spec.metrics.path` / `port` | `/metrics` / `http` | External scrape metadata |
| `spec.metrics.queueDepth` | Required when `metrics` is set | Runtime queue metric name |
| `spec.metrics.kvCacheUtil` / `running` / `ttft` | Optional | Additional external metric names |

When drain is enabled, Noctaya widens the termination grace period to at least `drainTimeout` plus 10 seconds. Runtime metric names are metadata only; autoscaling uses gateway demand.

`InferenceRuntime.status.conditions` is declared but not populated because the controller is passive. Operational state is reported on each consuming `LLMService`. See the [roadmap](../ROADMAP.md) for planned features.
