# Noctaya Roadmap

## Project status

Noctaya `v0.4.0-alpha.1` is the first prerelease under the current project, repository, API, image,
chart, and documentation coordinates. It packages the functional baseline completed in v0.3.0;
the v0.4.0 production-hardening objectives below are not complete. The core lifecycle works end to
end on real NVIDIA and Ascend accelerators, and the hardware-independent path runs in CI. The API
remains `serving.noctaya.io/v1alpha1`, so breaking changes are possible.

Noctaya is useful today for internal, development, and staging workloads where accelerator cost
matters, traffic can tolerate a cold start, and brief disruption is acceptable. It is not yet
intended for shared multi-tenant clusters, public customer-facing endpoints, or workloads that
require an availability or compatibility SLA.

### Main capabilities

- **Declarative model serving** — an `LLMService` selects a reusable `InferenceRuntime` and
  reconciles the backend, gateway, Services, cache, prewarm Job, and optional KEDA autoscaling.
- **Vendor-neutral runtime adaptation** — thin NVIDIA and Ascend adapters translate the same API
  into device-specific Kubernetes resources without implementing kernels, device plugins, or
  schedulers.
- **Scale-to-zero** — KEDA scales model backends from `0→1→N→0` using gateway demand. The
  compatibility path polls queue metrics; the opt-in ExternalScaler pushes cold activation
  immediately.
- **Cold-start-aware request handling** — the gateway holds or rejects cold requests, emits SSE
  heartbeats, bounds its queue, forwards streaming responses, and drains in-flight requests during
  scale-down.
- **Model delivery and caching** — Hugging Face, ModelScope, and pre-staged `pvc://` models work
  with `HostPath` or `NodeLocalPVC` caches and optional prewarming.
- **Composable operations** — runtime profiles can target vendor device plugins and Volcano;
  Prometheus and Grafana integration remains independent and deploys only when requested.

## Completed through v0.3.0

### Control plane and lifecycle

- The `LLMService` and cluster-scoped `InferenceRuntime` CRDs, reconciliation lifecycle, status
  conditions, ownership, runtime selection, and NVIDIA/Ascend adapter registry are implemented.
- Backend and gateway Deployments and Services, cache PVCs, prewarm Jobs, KEDA `ScaledObject`s,
  private-registry pull secrets, `pvc://` sources, and Volcano queue placement are supported.
- Helm and Kustomize installation paths, generated CRDs/RBAC, multi-architecture images, and
  versioned release artifacts are available.

### Scaling and request safety

- Queue-driven `0→1→N→0`, bounded admission (`429`), cold activation timeout (`503`), SSE
  keepalive, reject mode, load-gated readiness, and graceful drain are implemented.
- External-push mode removes KEDA's polling delay from cold activation and preserves demand across
  client disconnects and scaler reconnects with an activation lease.
- The CPU vLLM stub and Kind suites cover the complete lifecycle without an accelerator; unit,
  rendering, controller, gateway, and model-resolution tests run in CI.

### Hardware and ecosystem evidence

**Validated accelerators:** NVIDIA A10, NVIDIA A100, Ascend Atlas 300I Duo, and Ascend 910B3.

Together, these environments cover real inference, caching and prewarming, scale-to-zero,
multi-replica scale-out where the available topology permits it, admission limits, graceful drain,
Helm upgrades, recovery, and Volcano placement. Kind covers the no-accelerator lifecycle and
Volcano queue and quota tests.

See the [NVIDIA A10](docs/nvidia/a10-validation.md),
[Ascend 910B](docs/ascend/ascend-910b-validation.md), and
[Ascend 310P](docs/ascend/ascend-310p-validation.md) reports for exact stacks and evidence.

## v0.4.0-alpha.1 — identity migration prerelease

- Publishes the operator, gateway, chart, documentation site, examples, and automation under the
  Noctaya coordinates.
- Moves the alpha API to `serving.noctaya.io/v1alpha1` and updates Kubernetes names, labels,
  metrics, environment variables, and the queue endpoint consistently.
- Treats migration from v0.3.0 as a reinstall because no CRD conversion webhook is provided.
- Retains the v0.3.0 hardware reports as historical evidence. Repository and no-accelerator E2E
  validation do not create a new physical-accelerator claim.

## v0.4.0 — internal production hardening

The goal for v0.4.0 is to make Noctaya safer and more predictable for **controlled, single-tenant
internal production environments**. It will not by itself make Noctaya a general-purpose,
multi-tenant serving platform.

### Secure access and deployment boundaries

- Add gateway API-key authentication backed by Kubernetes Secrets, without storing credentials in
  `LLMService` objects or logs.
- Publish NetworkPolicy and TLS-termination guidance for the public gateway, backend Service,
  metrics endpoints, and cluster-internal ExternalScaler port.
- Review generated workload security contexts and RBAC, and add automated dependency,
  vulnerability, and release-artifact checks.

### High availability and failure recovery

- Support multiple gateway replicas without losing a complete demand signal, including an
  aggregation design for external-push activation.
- Add gateway disruption protection and placement controls such as a `PodDisruptionBudget` and
  topology-aware anti-affinity or spread constraints.
- Verify operator leader-election failover with multiple controller replicas and align the Helm
  defaults and documentation with the validated topology.
- Add soak and failure-injection coverage for gateway, operator, backend Pod, and node replacement.

### Predictable model delivery and multi-node scale-out

- Implement `SharedPVC` for a pre-populated RWX cache so replicas on new nodes do not each download
  the same model.
- Implement `oci://` model delivery for immutable and offline-friendly model packaging.
- Document and validate runtime-image pre-distribution and scale-down stabilization so an image
  pull is not repeatedly cancelled during bursty multi-node scale-out.

### Production operations

- Define actionable health, queue, activation, rejection, and drain signals, with optional alert
  examples and failure runbooks outside the core reconciler.
- Validate upgrade, rollback, component replacement, and cluster reboot procedures against the
  supported release path.
- Publish software bills of materials and provenance for release images, and evaluate image signing
  as part of the release workflow.

### v0.4.0 exit criteria

The release should not claim internal-production readiness until the following are demonstrated:

- unauthenticated inference requests are rejected when authentication is configured, and secret
  rotation does not require recreating an `LLMService`;
- a gateway or active operator replica can be removed without losing admitted requests or stopping
  reconciliation beyond the documented recovery window;
- a replica can start predictably on a fresh accelerator node using the documented image and model
  distribution path;
- upgrade and rollback instructions are exercised on a real accelerator environment; and
- the no-accelerator E2E suites, failure tests, and at least one representative real-hardware
  lifecycle pass for the release candidate.

## Future direction

### Stabilize the API from operational evidence

Move toward `v1beta1` only after the v0.4.0 operational work and external-user feedback clarify
which fields are durable. Add conversion or admission webhooks only when a concrete compatibility
requirement justifies their operational cost.

### Improve serving behavior where users need it

Potential demand-driven work includes KV-cache or latency-aware scaling, canary and blue-green
rollouts, LoRA lifecycle support, air-gapped model bundles, rate limiting, audit integration, and
tenant-aware policy. These features should be driven by real deployments rather than added to make
Noctaya resemble a larger platform.

### Remain a composable control plane

Noctaya will continue to own the Kubernetes model lifecycle and the small-cluster scale-to-zero
path. Fleet routing, prefill/decode disaggregation, datacenter scheduling, inference kernels,
device plugins, and schedulers remain outside its boundary. Noctaya should integrate with projects
such as KEDA, Volcano, HAMi, and Kthena instead of duplicating them.

### Grow through users and contributors

Priorities will be adjusted using deployment evidence, issue reports, and contributor interest.
The project will favor a small, well-tested core, reproducible hardware reports, and a sustainable
maintainer community over a broad speculative feature list.

## Current alpha limitations

- Cold starts take seconds to minutes; latency-critical models should use `scaling.min: 1`.
- The inference gateway has no built-in authentication and must remain behind a trusted boundary.
- External-push mode supports one gateway replica because demand is not yet aggregated.
- Node-local caches are per node; `SharedPVC` and `BakedImage` are not implemented.
- `oci://`, `s3://`, and `catalogRef` are not implemented.
- The `v1alpha1` API may change incompatibly.
- Hardware results apply only to the recorded device, topology, driver, runtime, and image stack.
