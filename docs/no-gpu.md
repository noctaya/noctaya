# Test without an accelerator

Noctaya's controllers, gateway, scaler integration, and scale-to-zero lifecycle can run on a CPU-only workstation. The E2E suite uses a fake accelerator resource and a CPU vLLM stub; it does not validate drivers, device plugins, vendor runtimes, performance, or physical hardware support.

## Prerequisites

Install:

- the Go version declared by `go.mod`;
- `make`;
- Docker, or Podman;
- Kind;
- `kubectl`; and
- Helm.

No existing Kubernetes cluster is required.

## Run local checks

```bash
make test
make lint
```

`make test` covers unit, envtest, adapter-rendering, gateway, model, and stub behavior. It also regenerates and formats source, then writes `cover.out`; inspect the working tree afterward.

## Run the scale-to-zero E2E suite

Run the External Push lifecycle:

```bash
make test-e2e
```

The command:

1. creates the disposable Kind cluster `noctaya-test-e2e`;
2. uses a dedicated kubeconfig;
3. installs KEDA `2.20.1`;
4. builds and loads the operator, gateway, and stub images;
5. runs the External Push lifecycle suite; and
6. deletes the cluster and kubeconfig on exit.

The runner refuses to reuse an existing cluster with that name or overwrite its kubeconfig.

The suite verifies:

- idle backend scale-to-zero;
- cold activation and streaming completion;
- concurrent scale-out through `0 → 1 → 2 → 0`;
- aggregate demand from two gateways during gateway replacement;
- backend Pod replacement, request recovery, and return to zero;
- backend image-pull failure reporting and recovery after correcting the runtime;
- graceful drain of an active stream;
- fast cold-start rejection with `503`; and
- push activation before the fallback polling interval;
- allowed and denied traffic through the opt-in ingress `NetworkPolicy` profile; and
- KEDA External Push over mutual TLS, including rejection of a plaintext scaler client.

CI runs the same lifecycle through `.github/workflows/test-e2e.yml`.

## Use Podman

Select Podman for both the image build and the Kind provider:

```bash
KIND_EXPERIMENTAL_PROVIDER=podman make test-e2e CONTAINER_TOOL=podman
```

## CPU vLLM stub

`test/vllm-stub/` provides only the interfaces needed by the suite:

| Endpoint | Purpose |
|---|---|
| `/health` | Delayed readiness for cold-start tests |
| `/v1/chat/completions` and `/v1/completions` | JSON and streaming SSE responses |
| `/metrics` and `/control` | Runtime-shaped metrics and test-time metric updates |

Run its focused tests with:

```bash
go test ./test/vllm-stub/...
```

Physical accelerator claims must follow the [hardware validation requirements](validation/requirements.md).
