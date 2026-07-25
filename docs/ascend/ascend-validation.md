# Ascend hardware validation

This page is the entry point for validating Noctaya on real Ascend servers. Product-specific
commands live in the [910B report](ascend-910b-validation.md) and the
[310P runbook](ascend-310p-validation.md).

## What must be present

| Component | 910B | 310P |
|---|---|---|
| vLLM image | `quay.io/ascend/vllm-ascend:v0.21.0rc1` | `quay.io/ascend/vllm-ascend:v0.22.1rc1-310p` |
| Device resource | `huawei.com/Ascend910` | `huawei.com/Ascend310P` |
| Node label | `accelerator=huawei-Ascend910` plus the Noctaya product label | `accelerator=huawei-Ascend310P` plus the Noctaya product label |
| Smoke model | `Qwen/Qwen2.5-0.5B-Instruct` | `Qwen/Qwen2.5-0.5B-Instruct` |
| Noctaya operator | `ghcr.io/noctaya/noctaya:<release>` | `ghcr.io/noctaya/noctaya:<release>` |
| Noctaya gateway | `ghcr.io/noctaya/noctaya-gateway:<release>` | `ghcr.io/noctaya/noctaya-gateway:<release>` |
| Cluster services | Ascend Device Plugin and KEDA images compatible with the cluster | Ascend Device Plugin and KEDA images compatible with the cluster |

Use the same release tag for both Noctaya images. The prewarm Job reuses the vLLM image, and the
smoke model is downloaded into that container rather than packaged as an image.
`kube-prometheus-stack` is optional unless metrics collection is part of the validation. Noctaya
does not pin the device-plugin or KEDA images because their compatible versions depend on the host
and cluster; record the exact images and digests used by the validation environment.

Before deployment, record the server model, OS, architecture, firmware, driver, CANN, device-plugin,
container-runtime, Kubernetes, and KEDA versions. Also record every image by digest. Tags alone are
not reproducible evidence.

## Current status

| Profile | Highest completed level | Remaining work |
|---|---|---|
| Ascend 910B3 | Scale-to-zero verified on one physical device on 2026-07-15 | Revalidate new stacks; test `1→N` on a multi-device server |
| Atlas 300I Duo | Scale-to-zero verified on 2026-07-14 | Revalidate each new driver, device-plugin, or runtime-image combination |
| Atlas 300I Pro | Rendering-tested | Runtime, integrated, and scale-to-zero validation |

Separate component tests do not combine into a higher validation level. For example, a runtime test
plus a gateway test is not an integrated operator-to-device-plugin result.

## Validation levels

Use these terms consistently in issues, documentation, and release notes:

1. **Rendering-tested**: unit tests confirm the expected Pod, resource request, mounts, and probes.
2. **Runtime-tested**: vLLM serves a model on the accelerator outside Noctaya's full lifecycle.
3. **Integrated**: the operator schedules the Pod through the device plugin and inference succeeds
   through the Noctaya gateway.
4. **Scale-to-zero verified**: KEDA and the gateway complete `0 → 1 → configured maximum → 0`,
   including cold-start handling and an in-flight drain. State the device count when the maximum is
   one; that result is not evidence for multi-replica scaling.

Only level 4 supports an end-to-end hardware-validation claim.

## Required evidence

For each server profile, retain:

- `kubectl describe node` showing the allocatable Ascend resource;
- the rendered backend Deployment and selected `InferenceRuntime`;
- prewarm, backend, gateway, operator, device-plugin, and KEDA logs;
- a completed streaming response ending in `[DONE]`;
- timestamps for image pull, model prewarm, Pod readiness, cold start, and scale-down;
- evidence that an in-flight stream completes during Pod termination; and
- the final `LLMService` status and relevant Prometheus metrics.

Do not reuse results across 910B, Atlas 300I Duo, and Atlas 300I Pro. Each hardware and software
combination needs its own evidence set.
