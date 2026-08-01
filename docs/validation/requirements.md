# Hardware validation requirements

Noctaya hardware-support claims must be backed by reproducible tests on a named physical accelerator. Rendering tests verify Kubernetes output; they are not hardware evidence. Use these requirements for NVIDIA, Ascend, and future accelerator backends.

## Validation levels

Use one of these terms in reports, issues, and release notes:

| Level | Meaning |
|---|---|
| Rendering-tested | Unit tests confirm the expected workload, resources, mounts, and probes. |
| Runtime-tested | The inference runtime serves a model on the accelerator outside Noctaya's full lifecycle. |
| Integrated | Noctaya schedules the backend through the device plugin and inference succeeds through the gateway. |
| Scale-to-zero verified | KEDA and the gateway complete `0 → 1 → configured maximum → 0`, including cold-start handling and an in-flight drain. |

Only the final level supports an end-to-end hardware-validation claim. A single-device `0 → 1 → 0` result does not prove multi-replica scaling.

## Record the baseline

Capture the following before testing:

- accelerator product, count, memory, topology, and allocation mode;
- host model, architecture, operating system, and kernel;
- firmware, driver, vendor runtime, device-plugin version, Kubernetes resource name, and node labels;
- container runtime, Kubernetes, Helm, KEDA, and any optional scheduler or sharing integration;
- Noctaya operator, gateway, and inference-runtime images, including tags and digests;
- model identifier and revision, cache configuration, External Push settings, and replica limits;
- the exact `InferenceRuntime` and `LLMService` manifests used.

Tags alone are not reproducible evidence. Record image digests whenever the registry exposes them.

## Minimum end-to-end checks

A scale-to-zero report must show:

1. The device plugin advertises the expected allocatable resource and assigns it to the backend.
2. Prewarming and cache reuse work when enabled.
3. The backend starts at zero while the gateway remains ready.
4. A cold request activates the backend and returns real model output; a streaming response ends with `[DONE]`.
5. Demand reaches the configured replica maximum when capacity permits, with distinct device allocations for a multi-device claim.
6. A warm request succeeds, then the backend returns to zero and releases its accelerator.
7. An in-flight stream completes during backend termination within the configured drain timeout.
8. Admission limits and activation failure behavior are observable and bounded.

Release candidates must also follow the [release validation checklist](releases.md).

## Evidence to retain

Keep the commands and outputs needed to reproduce the result, including:

- node capacity and allocation details;
- rendered workloads and the selected runtime;
- operator, gateway, backend, prewarm, KEDA, and device-plugin logs;
- request status, response output, and relevant metrics;
- timestamps for image pull, prewarm, readiness, cold start, and scale-down; and
- final `LLMService` status, failures, workarounds, and untested scenarios.

Do not combine separate component tests into a higher validation level or reuse results across devices, topologies, or software stacks. Optional integrations are supported only to the extent recorded in the report.

## Device reports

- [NVIDIA A10](nvidia/a10.md)
- [Ascend 310P](ascend/310p.md)
- [Ascend 910B3](ascend/910b3.md)
