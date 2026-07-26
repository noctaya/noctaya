# Noctaya documentation

Noctaya is a minimal, composable LLM serving control plane for private Kubernetes clusters. It turns an `LLMService` and a reusable `InferenceRuntime` into a cold-start-aware gateway, a model
backend that can scale to zero, optional model caching and prewarming, and KEDA autoscaling resources.

## Start here

- [Getting started](getting-started.md) covers installation, a first profile deployment, prewarming, request-time activation, and the return to zero.
- [Examples and model configuration](../examples/README.md) covers NVIDIA and Ascend profiles and explains how to change the served model.
- [Architecture](architecture.md) explains the component boundary, reconciliation output, and scale-to-zero lifecycle.
- [CRD reference](crd.md) documents the `LLMService` and `InferenceRuntime` API.

## Operate and validate

- [Testing without an accelerator](no-gpu.md) exercises the control plane and scale-to-zero path on Kind.
- [Hardware validation](validation/requirements.md) explains the evidence required for physical accelerator claims.

## Project

Read the [roadmap](../ROADMAP.md) for current limitations and v0.4.0 priorities. Contributions are welcome; the [contributing guide](../CONTRIBUTING.md) explains the development workflow and project boundary.
