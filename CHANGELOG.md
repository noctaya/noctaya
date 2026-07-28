# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow
[Semantic Versioning](https://semver.org/) once it reaches a stable release.

## [Unreleased]

### Changed
- Made KEDA External Push the sole scaler transport. Cold `0→1` activation is streamed immediately, while periodic metric reads remain responsible for recovery and `1→N` scale-out.
- Removed the `gateway.scalerMode`, `--scaler-mode`, and dual-mode E2E configuration surfaces. Remove stored `gateway.scalerMode` values before upgrading the Helm release.
- Restricted gateway replicas to one until demand aggregation is implemented.

### Fixed
- Serialized gateway demand notifications so concurrent transitions cannot publish stale scaler state.
- Made a missing KEDA `ScaledObject` CRD an explicit reconciliation failure and added the `AutoscalingReady` condition.
- Added signal-aware HTTP and gRPC shutdown to the per-model gateway.

### Tests
- Changed the Kind lifecycle to prove activation occurs before a deliberately extended fallback polling interval.

## [0.4.0-alpha.1] - 2026-07-25

This is the first Noctaya-branded prerelease. It publishes the v0.3.0 functional baseline under
the current repository, API, image, chart, namespace, metrics, and documentation coordinates while
v0.4.0 production-hardening work continues. It remains alpha software and is not production-ready.

### Changed
- Adopted the Noctaya identity across the Go module, binaries, container images, Helm chart,
  Kubernetes object names, documentation website, examples, tests, and release automation.
- Changed the alpha API group to `serving.noctaya.io`, the gateway queue endpoint to
  `/noctaya/queue`, gateway metrics to the `noctaya_gateway_*` namespace, and gateway environment
  variables to the `NOCTAYA_*` prefix. Existing alpha resources require explicit migration; no
  conversion path is provided.

### Migration
- Treat this prerelease as a reinstall rather than an in-place upgrade from v0.3.0. Back up custom
  resource specifications and model data, install the current chart, recreate resources with
  `serving.noctaya.io/v1alpha1`, verify the new workloads, and only then remove the previous
  installation.
- Preserve the existing v0.3.0 tag and release as historical artifacts. The renamed chart and
  images start at v0.4.0-alpha.1.

### Verified
- The renamed repository passed generation, build, unit and controller tests, lint, documentation
  build, Helm and Kustomize rendering, and both disposable no-accelerator E2E scaler modes.
- Physical accelerator claims remain tied to their recorded release, runtime, driver, device
  plugin, and topology. This identity prerelease does not claim a new real-hardware validation run.

## [0.3.0] - 2026-07-19

This alpha release adds immediate push-based cold activation, a current NVIDIA A10 profile, and
expanded real-accelerator evidence for Noctaya alongside Volcano and Kthena. The API remains
`v1alpha1`, and Noctaya is not production-ready for shared or customer-facing workloads.

### Added
- An opt-in, co-located KEDA ExternalScaler that streams cold activation immediately while retaining
  queue metrics for scale-out, scale-down, observability, and rollback.
- An activation lease for reject-mode cold starts, including current-state replay across scaler
  reconnects and a no-GPU E2E mode for the external-push path.
- An NVIDIA A10 profile and reproducible K3s validation report for vLLM `v0.25.1`.
- A hardware-neutral, command-driven Noctaya and Kthena demo showing a hot model alongside a
  long-tail model that activates from zero and returns to zero.

### Changed
- Helm and the manager accept `gateway.scalerMode` / `--scaler-mode`; `metrics-api` remains the
  compatibility default and `external-push` requires one gateway replica.
- The A100 example now uses device-specific names, vLLM `v0.25.1`, the positional model argument,
  and a conservative one-replica default. Its historical hardware evidence remains tied to vLLM
  `v0.22.0` pending focused revalidation of the new stack.
- The primary quickstart now uses the physically validated A10 profile.

### Removed
- The redundant standalone A100 HostPath manifest. `cache.strategy: HostPath` remains available in
  the API; bundled profiles consistently use `NodeLocalPVC` and require a dynamic StorageClass.

### Fixed
- Use vLLM's current `vllm:kv_cache_usage_perc` metric in all runtime profiles, the CPU stub, and
  the optional Grafana dashboard.
- Correct the recorded Ascend 910B3 identity and tighten the evidence boundaries in the 910B3 and
  310P validation reports.
- Keep prerelease image publication from moving the stable `latest` operator and gateway tags.

### Verified
- The v0.3.0-rc.1 release build completed the full Noctaya lifecycle on two physical NVIDIA A10 GPUs
  with vLLM `v0.25.1`, KEDA external-push, prewarming, `0→1→2→0`, bounded admission, reject mode,
  graceful drain, component replacement, and host reboot recovery.
- Volcano `v1.15.0` enforced queue placement and quota on Kind, then scheduled the real A10
  workloads through separate queues and placed two Noctaya replicas on distinct whole GPUs.
- A Kthena-managed hot model and a Noctaya-managed long-tail model served concurrently on the same
  host. Noctaya recovered automatically after reboot; the documented Kthena device-admission race
  required manual Pod replacement and remains outside Noctaya's controller boundary.

## [0.3.0-rc.1] - 2026-07-19

This release candidate adds immediate push-based cold activation and a physically validated NVIDIA
A10 profile while aligning bundled vLLM metrics and NVIDIA examples with current runtime behavior.
Noctaya remains alpha software with a `v1alpha1` API and is not production-ready.

### Added
- An opt-in, co-located KEDA ExternalScaler that streams cold activation immediately while retaining
  queue metrics for scale-out, scale-down, observability, and rollback.
- An activation lease for reject-mode cold starts, including current-state replay across scaler
  reconnects and a no-GPU E2E mode for the external-push path.
- An NVIDIA A10 profile and reproducible K3s validation report for vLLM `v0.25.1`.

### Changed
- Helm and the manager accept `gateway.scalerMode` / `--scaler-mode`; `metrics-api` remains the
  compatibility default and `external-push` requires one gateway replica.
- The A100 example now uses device-specific `vllm-nvidia-a100` / `qwen3-8b-a100` names, vLLM
  `v0.25.1`, the positional model argument, and a conservative one-replica default. Its historical
  hardware evidence remains tied to vLLM `v0.22.0` pending focused revalidation of the new stack.

### Removed
- The redundant standalone A100 HostPath manifest. `cache.strategy: HostPath` remains available in
  the API; bundled profiles consistently use `NodeLocalPVC` and require a dynamic StorageClass.

### Fixed
- Use vLLM's current `vllm:kv_cache_usage_perc` metric in all runtime profiles, the CPU stub, and
  the optional Grafana dashboard.
- Correct the recorded Ascend 910B3 identity and tighten the evidence boundaries in the 910B3 and
  310P validation reports.
- Keep prerelease image publication from moving the stable `latest` operator and gateway tags.

### Verified
- Two physical NVIDIA A10 GPUs completed the whole-device `0→1→2→0` lifecycle with real vLLM
  tokens, prewarming, cache persistence, backpressure, draining, recovery, and scale-down to zero.
- Volcano `v1.15.0` on a three-node Kind cluster completed queue placement, quota enforcement, and
  Noctaya's `0→1→0` path with the CPU stub and a fake extended resource. Real-accelerator topology,
  HAMi sharing, and gang scheduling remain separate validation work.

## [0.2.0] - 2026-07-17

Alpha release completing Noctaya's first integrated Ascend hardware-validation cycle and hardening
the scale-to-zero control plane based on those results. Atlas 300I Duo is verified through the
multi-replica `0→1→2→0` lifecycle, while Ascend 910B3 is verified through the complete
single-device `0→1→0` lifecycle. Noctaya remains `v1alpha1` and is not production-ready.

### Added
- A shared Ascend hardware-validation guide covering required images, validation levels, evidence,
  and separate 910B, Atlas 300I Duo, and Atlas 300I Pro result sets.
- Full physical 910B3 evidence for device-plugin scheduling, single-device `0→1→0`, cold and warm
  inference, reject mode, bounded-queue backpressure, metrics, drain, self-heal, Helm upgrade, and
  reboot recovery.
- Full Atlas 300I Duo hardware evidence for the integrated `0→1→2→0` lifecycle, inference,
  backpressure, reject mode, drain, cache persistence, self-heal, Helm upgrade, and reboot recovery.
- Validation bounds for accelerator counts, scaling values, runtime ports, and termination grace
  periods.

### Changed
- Prometheus Operator resources and RBAC are decoupled from core reconciliation. Noctaya now exposes
  stable metrics discovery labels while optional `ServiceMonitor` and Grafana assets live under
  `examples/observability/`.
- Runtime metric fields are optional metadata for external observability; backend adapters and
  autoscaling no longer carry an unused runtime-metrics dependency.
- `InferenceRuntime` family and vendor enums now match the built-in implementations: vLLM on
  NVIDIA or Ascend. Manager RBAC is reduced to the CRD operations used by the passive runtime and
  service reconcilers.
- Ascend device profiles use equal priority, so clusters with multiple installed profiles must pin
  the intended hardware runtime instead of silently preferring 910B.
- Runtime and service examples now live in independently deployable, vendor/device-specific
  `examples/` directories, with concise filenames that leave the device model to the directory.
- Prewarm Jobs inherit the runtime's node selector, tolerations, scheduler, and Volcano queue so
  node-local model data is prepared where the backend can run.
- `LLMService` resources are reconciled when a matching `InferenceRuntime` changes.
- The Ascend 910B runtime now invokes `vllm serve` explicitly and uses MindCluster's standard
  `accelerator=huawei-Ascend910` node label. A dedicated hardware-validation service example uses a
  60-second drain, the shortest tested value that completed a live 910B3 stream without aborting.
- Ascend 310P examples invoke `vllm serve` explicitly and pin FP16 for the validated 310P3 path.

### Fixed
- Keep retrying unchanged reconciliation failures, reject ambiguous equal-priority runtime
  selections, and reject Volcano queue names unless the Volcano scheduler is selected.
- Apply `scaleDownStabilization` to both KEDA's zero cooldown and HPA scale-down behavior, with
  validation for the HPA's one-hour limit.
- Build multi-platform images on the build host architecture and fail the `docker-buildx` target
  when the image build fails.
- Embed the release tag in operator and gateway images built by the release workflow.
- Publish the release chart only after both release images build successfully.
- Align Helm manager pod security and termination settings with the Kustomize deployment.
- Pin Kind and KEDA in E2E workflows, install Helm explicitly, and fail CI on uncommitted module or
  generated-file changes.
- Make repeated Podman scale-to-zero E2E runs replace and clean their temporary image archives.
- Reject unsupported `BakedImage` cache requests instead of silently rendering them as uncached.
- Reject invalid PVC claim names and model paths that could escape the mounted model volume.
- Prevent accelerator-free prewarm Pods from autoloading PyTorch vendor backends and requiring host
  driver libraries.
- Report the committed SSE `200` for keepalive activation timeouts and avoid classifying canceled
  clients as activation timeouts in gateway metrics.

## [0.2.0-rc.1] - 2026-06-27

Pre-release documenting the first **real-hardware bring-up of the Ascend 910B backend**. vLLM-Ascend
now serves on a physical 910B, the operator's rendered manifests are confirmed correct for the 910
family, and the gateway data-plane is verified against the live NPU. The remaining gap — the operator
scheduling a backend pod onto an NPU via the device plugin (full integrated scale-to-zero e2e) — needs
a schedulable NPU node and stays open. Ascend support is therefore **experimental / technical preview**,
not yet "supported." Still `v1alpha1` and not production-ready.

### Added
- **Ascend 910B validation report + bring-up runbook** ([docs/validation/ascend/910b3.md](docs/validation/ascend/910b3.md))
  capturing the verified environment (910B3 64 GB, CANN 9.0.0 / driver 26.0.rc1), the smoke test,
  the operator render dry-run, and the gateway data-plane results.

### Changed
- **Ascend runtime image** pinned to `quay.io/ascend/vllm-ascend:v0.21.0rc1` (was `v0.18.0`) — the
  base Atlas-A2/910B tag, matching the `vllm_ascend 0.21.0rc1` stack verified on real hardware.
- **Ascend status** updated from "scaffolded, not run on hardware" to **experimental / technical
  preview** across README, ROADMAP, and the adapter docs, reflecting what is now verified on a real
  910B (vLLM-Ascend serves; manifests render correctly; gateway data-plane works) versus what is not
  (device-plugin scheduling + full integrated e2e).

### Verified (Ascend 910B, real hardware)
- **vLLM-Ascend serves on the NPU** — Qwen2.5 loaded onto a 910B3 and answered via the OpenAI API
  (CANN 9.0.0 / driver 26.0.rc1, vllm-ascend 0.21.0rc1).
- **Operator renders a correct 910B backend** (kind dry-run) — `huawei.com/Ascend910` request, CANN
  driver host-mounts, ModelScope cache wiring, load-gated probes; vendor selector resolves to `vllm-ascend`.
- **Gateway data-plane on real NPU** — `/healthz`, `/noctaya/queue` (incl. demand-linger), `/metrics`,
  OpenAI passthrough (streaming + non-streaming), and cold-start SSE keepalive → activation timeout → `503`.

### Not yet verified
- Operator → Ascend device plugin → backend pod **scheduled and serving on the NPU**, and the full
  integrated `0→1→N→0` loop on a real NPU node (the v1 "supported" milestone).

## [0.1.0] - 2026-06-06

First **release (alpha)**. The core thesis — declarative, scale-to-zero serving of self-hosted
open-source LLMs on Kubernetes — is implemented and verified end-to-end on real NVIDIA GPUs, and
the full loop now runs in CI with no hardware.
Not production-ready (see [ROADMAP.md](ROADMAP.md)).

### Added
- **CRDs** — `LLMService` (namespaced) and `InferenceRuntime` (cluster-scoped, pluggable backend),
  API group `serving.noctaya.io/v1alpha1`.
- **NVIDIA backend** — renders the vLLM serving workload (image, templated args, accelerator
  resource, model-load-aware probes, metrics). **Ascend** adapter scaffolded + golden-tested
  (not yet run on NPUs).
- **Scale-to-zero** — Noctaya gateway (buffering reverse proxy) + KEDA `ScaledObject` on gateway
  queue depth; `0→1→N→0`, verified `1→2` across two GPU nodes.
- **Cold-start handling** — SSE keepalive heartbeats, `reject` mode, load-gated readiness, bounded
  queue (`429`) and activation timeout (`503`).
- **Graceful drain** — in-flight streams finish before scale-down.
- **Model caching** — `HostPath` and `NodeLocalPVC` (with pinnable `cache.storageClassName`,
  verified against Alibaba ESSD) + a prewarm Job.
- **Observability** — per-gateway Prometheus metrics, `ServiceMonitor`, and a Grafana dashboard.
- **No-GPU test harness** — a CPU `vllm-stub` and a kind + KEDA e2e that runs the full
  `0→1→N→0` loop, backpressure (`429`/`503`), and graceful drain on every PR, no accelerator
  required, plus a [no-GPU development guide](docs/no-gpu.md).
- **Packaging** — Helm chart (operator + RBAC + CRDs) and multi-arch image build/release workflow.
- **Project scaffolding** — README, ROADMAP, CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, MAINTAINERS,
  GOVERNANCE, issue/PR templates, and a DCO check.

### Changed
- Operator skips no-op `LLMService` status updates, avoiding optimistic-concurrency churn.

[Unreleased]: https://github.com/noctaya/noctaya/compare/v0.4.0-alpha.1...HEAD
[0.4.0-alpha.1]: https://github.com/noctaya/noctaya/compare/v0.3.0...v0.4.0-alpha.1
[0.3.0]: https://github.com/noctaya/noctaya/compare/v0.3.0-rc.1...v0.3.0
[0.3.0-rc.1]: https://github.com/noctaya/noctaya/compare/v0.2.0...v0.3.0-rc.1
[0.2.0]: https://github.com/noctaya/noctaya/compare/v0.2.0-rc.1...v0.2.0
[0.2.0-rc.1]: https://github.com/noctaya/noctaya/compare/v0.1.0...v0.2.0-rc.1
[0.1.0]: https://github.com/noctaya/noctaya/releases/tag/v0.1.0
