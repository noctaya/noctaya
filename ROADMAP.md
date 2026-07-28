# Noctaya Roadmap

## Project status

Noctaya is an alpha Kubernetes control plane for internal, development, and staging workloads where accelerator cost matters and traffic can tolerate a cold start. It is not yet intended for shared multi-tenant clusters, public customer-facing endpoints, or workloads that require an availability or compatibility SLA. The API remains `serving.noctaya.io/v1alpha1` and may change incompatibly.

### Current boundaries

- Cold starts can take seconds to minutes; latency-sensitive models should use `scaling.min: 1`.
- The gateway has no built-in authentication and must remain behind a trusted boundary.
- Multiple gateways use one replaceable per-model demand aggregator; disruption protection and broader failure-injection coverage are still in progress.
- Node-local caches are per node; shared and immutable model distribution is not yet available.
- Hardware support claims remain specific to the validated device, topology, driver, runtime, and image stack.

---

## Problems and core capabilities

Long-tail models often reserve expensive accelerators while idle. Simply scaling a Deployment to zero does not solve the return path: the next request must survive scheduling, image pull, model loading, and readiness without overwhelming the gateway. Serving configuration also tends to become tied to one device vendor or a larger platform.

Noctaya addresses these problems with a small Kubernetes-native lifecycle layer:

- **Declarative serving** — a namespaced `LLMService` selects a reusable, cluster-scoped `InferenceRuntime`, separating model intent from cluster and device configuration.
- **Scale-to-zero lifecycle** — an always-on gateway exposes an OpenAI-compatible endpoint while independently installed KEDA scales the model backend through `0→1→N→0`.
- **Cold-start safety** — bounded admission, activation leases, SSE heartbeats, model-aware readiness, and graceful drain protect requests and the gateway throughout activation and scale-down.
- **Model delivery and caching** — Hugging Face, ModelScope, and pre-staged `pvc://` sources work with `HostPath` or `NodeLocalPVC` caches and optional prewarming.
- **Thin accelerator adaptation** — NVIDIA and Ascend adapters translate the same API into device-specific Kubernetes resources without implementing kernels, runtimes, or device plugins.
- **Composable operations** — device plugins, KEDA, schedulers such as Volcano, and optional Prometheus and Grafana resources remain independently installed and managed.

---

## v0.4.0 — controlled production hardening

The v0.4.0 goal is to make Noctaya safer and more predictable for controlled, single-tenant internal production environments. It will not make Noctaya a general-purpose multi-tenant serving platform.

### Security and deployment boundaries

- Add gateway API-key authentication backed by Kubernetes Secrets, including safe secret rotation.
- Publish NetworkPolicy and TLS-termination guidance for the gateway, backend, metrics, and cluster-internal ExternalScaler endpoint.
- Review workload security contexts and RBAC, and automate dependency, vulnerability, and release artifact checks.

### Availability and recovery

- Harden aggregate-scaler replacement and network-failure recovery with broader failure-injection coverage.
- Add disruption and placement controls, including a `PodDisruptionBudget` and topology-aware spreading.
- Validate operator leader-election failover and add soak and failure-injection coverage for gateway, operator, backend, and node replacement.

### Predictable model delivery

- Add `SharedPVC` support for pre-populated RWX model caches across nodes.
- Add `oci://` model delivery for immutable and offline-friendly packaging.
- Document and validate runtime-image pre-distribution and scale-down stabilization for fresh-node activation.

### Production operations

- Define actionable health, queue, activation, rejection, and drain signals with optional alerts and failure runbooks.
- Validate upgrade, rollback, component replacement, and cluster reboot procedures.
- Publish release SBOMs and provenance, and evaluate image and chart signing.

---

## Future direction

- **Stabilize from operational evidence** — move toward `v1beta1` only after production-hardening work and external feedback identify durable API fields.
- **Improve serving behavior on demand** — consider KV-cache or latency-aware scaling, canary and blue-green rollouts, LoRA lifecycle support, rate limiting, audit integration, and tenant-aware policy when real deployments justify them.
- **Remain composable** — keep fleet routing, prefill/decode disaggregation, datacenter scheduling, inference kernels, device plugins, schedulers, and monitoring outside Noctaya's ownership.
- **Grow with users and contributors** — favor a small, well-tested core, reproducible hardware evidence, and a sustainable maintainer community over speculative breadth.
