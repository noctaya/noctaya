# Noctaya Roadmap

## Project status

Noctaya is an alpha Kubernetes control plane for internal, development, and staging workloads where accelerator cost matters and traffic can tolerate a cold start. It is not yet intended for shared multi-tenant clusters, public customer-facing endpoints, or workloads that require an availability or compatibility SLA. The API remains `serving.noctaya.io/v1alpha1` and may change incompatibly.

### Current boundaries

- Cold starts can take seconds to minutes; latency-sensitive models should use `scaling.min: 1`.
- Gateway API-key authentication is optional; unauthenticated endpoints must remain behind a trusted boundary.
- Multiple gateways use one replaceable per-model demand aggregator; broader failure-injection coverage is still in progress.
- Shared RWX caches and digest-pinned OCI model artifacts are available, but portability remains specific to the validated StorageClass and registry/runtime combination.
- Hardware support claims remain specific to the validated device, topology, driver, runtime, and image stack.

---

## Problems and core capabilities

Long-tail models often reserve expensive accelerators while idle. Simply scaling a Deployment to zero does not solve the return path: the next request must survive scheduling, image pull, model loading, and readiness without overwhelming the gateway. Serving configuration also tends to become tied to one device vendor or a larger platform.

Noctaya addresses these problems with a small Kubernetes-native lifecycle layer:

- **Declarative serving** — a namespaced `LLMService` selects a reusable, cluster-scoped `InferenceRuntime`, separating model intent from cluster and device configuration.
- **Scale-to-zero lifecycle** — an always-on gateway exposes an OpenAI-compatible endpoint while independently installed KEDA scales the model backend through `0→1→N→0`.
- **Cold-start safety** — configurable bounded admission, activation leases, SSE heartbeats, model-aware readiness, and graceful drain protect requests and the gateway throughout activation and scale-down.
- **Gateway availability and access** — optional Secret-backed API-key authentication, preferred cross-node placement, and disruption budgets protect public gateway replicas without coupling Noctaya to an ingress implementation.
- **Model delivery and caching** — Hugging Face, ModelScope, digest-pinned OCI artifacts, and pre-staged `pvc://` sources work with node-local or shared persistent caches and controlled prewarming.
- **Thin accelerator adaptation** — NVIDIA and Ascend adapters translate the same API into device-specific Kubernetes resources without implementing kernels, runtimes, or device plugins.
- **Composable operations** — device plugins, KEDA, schedulers such as Volcano, and optional Prometheus and Grafana resources remain independently installed and managed.

---

## v0.4.0 — controlled production hardening

The v0.4.0 goal is to make Noctaya safer and more predictable for controlled, single-tenant internal production environments. It will not make Noctaya a general-purpose multi-tenant serving platform.

### Security and deployment boundaries

- Publish NetworkPolicy and TLS-termination guidance for the gateway, backend, metrics, and cluster-internal ExternalScaler endpoint.
- Review workload security contexts and RBAC, and automate dependency, vulnerability, and release artifact checks.

### Availability and recovery

- Harden aggregate-scaler replacement and network-failure recovery with broader failure-injection coverage.
- Extend placement controls beyond preferred host anti-affinity where accelerator topology requires it.
- Add longer soak and failure-injection coverage beyond the release-gated gateway, operator, backend, and node replacement paths.

### Predictable model delivery

- Document and validate runtime-image pre-distribution and scale-down stabilization for fresh-node activation.

### Production operations

- Define actionable health, queue, activation, rejection, and drain signals with optional alerts and failure runbooks.
- Extend release recovery evidence to additional Kubernetes distributions and physical accelerator stacks.
- Publish release SBOMs and provenance, and evaluate image and chart signing.

---

## Future direction

- **Stabilize from operational evidence** — move toward `v1beta1` only after production-hardening work and external feedback identify durable API fields.
- **Improve serving behavior on demand** — consider KV-cache or latency-aware scaling, canary and blue-green rollouts, LoRA lifecycle support, rate limiting, audit integration, and tenant-aware policy when real deployments justify them.
- **Remain composable** — keep fleet routing, prefill/decode disaggregation, datacenter scheduling, inference kernels, device plugins, schedulers, and monitoring outside Noctaya's ownership.
- **Grow with users and contributors** — favor a small, well-tested core, reproducible hardware evidence, and a sustainable maintainer community over speculative breadth.
