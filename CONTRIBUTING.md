# Contributing to Noctaya

Thank you for helping improve Noctaya. The project is still alpha, so focused bug fixes, tests,
documentation, hardware evidence, and small design improvements are especially valuable.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). Contributions are
accepted under the [Apache-2.0 license](https://github.com/noctaya/noctaya/blob/main/LICENSE)
and must include a
[Developer Certificate of Origin](https://developercertificate.org/) (DCO) sign-off.

## Before you start

- Read the [architecture](docs/architecture.md) to understand Noctaya's component boundary.
- Check the [roadmap](ROADMAP.md), existing issues, and pull requests before starting overlapping
  work.
- Open an issue before changing an API, adding a dependency or backend vendor, moving component
  boundaries, or beginning a large refactor.
- Report suspected vulnerabilities privately by following the [security policy](SECURITY.md).

Look for issues labeled `good first issue` or `help wanted` if you are looking for a starting point.
Bug reproductions, documentation fixes, examples, and real-hardware reports are all useful
contributions.

## Project boundary

Noctaya owns the Kubernetes orchestration and lifecycle layer for scale-to-zero LLM serving. This
includes declarative workloads, runtime selection, model caching, health, accelerator scheduling
translation, request-aware activation, and stable metrics surfaces.

Keep inference kernels, vendor runtime behavior, device plugins, schedulers, fleet routing, and
monitoring lifecycle in their respective upstream projects. Vendor adapters in Noctaya should stay
thin and translate only the Kubernetes details required by an `InferenceRuntime`.

## Development environment

The basic development loop requires:

- Go at the version declared by
  [`go.mod`](https://github.com/noctaya/noctaya/blob/main/go.mod) (currently Go 1.26);
- Git and `make`;
- Docker, or Podman through `CONTAINER_TOOL=podman`, for image and scale-to-zero tests; and
- `kubectl`, Kind, and Helm for cluster-based tests.

Clone the repository and run the standard checks:

```bash
git clone https://github.com/noctaya/noctaya.git
cd noctaya
make build
make test
make lint
```

The Makefile downloads pinned development tools into `bin/`. Both `make build` and `make test`
regenerate manifests and code and run formatting and vet; `make test` also downloads envtest assets
and writes `cover.out`. Always inspect `git diff` afterward so generated or formatted changes are
intentional.

### Focused tests

Use the closest package while iterating, then run the standard checks before opening a pull
request. Common commands include:

```bash
go test ./internal/backend/...
go test ./internal/gateway/...
go test ./internal/model/...
go test ./test/vllm-stub/...
```

Controller tests require envtest binaries, so use `make test` unless `KUBEBUILDER_ASSETS` is
already configured.

### Develop without an accelerator

Most behavior can be developed without a GPU or NPU. The CPU vLLM stub covers request handling and
the complete scale-to-zero lifecycle; see [Developing Noctaya without a GPU](docs/no-gpu.md).

To run the controller manually, use a disposable cluster and verify the current context before
installing anything:

```bash
kind create cluster --name noctaya-dev
kubectl config current-context  # must report kind-noctaya-dev
make install
make run
```

Hardware profiles often enable prewarming and can download large model weights. Do not apply one
to a laptop merely to inspect its manifests. Render it locally instead:

```bash
kubectl kustomize examples/nvidia/a100 >/dev/null
```

Use the no-GPU guide and `test/scale-to-zero/` suite when you need the operator, gateway, KEDA, and a
schedulable CPU backend together. Rendering a workload successfully is not hardware validation.

### End-to-end tests

The E2E runner supports both scaler modes:

| Check | Command | Environment |
|---|---|---|
| Default scale-to-zero path | `make test-e2e` | Creates an isolated Kind cluster, installs KEDA, runs the lifecycle, and cleans up |
| External-push path | `make test-e2e E2E_SCALER_MODE=external-push` | Creates the same isolated environment and exercises the streaming ExternalScaler |

The runner uses a dedicated kubeconfig, refuses to reuse an existing cluster, and cleans up on
success, failure, or interruption. Never point E2E tooling at development, staging, or production
clusters.

## Project layout

| Path | Responsibility |
|---|---|
| `api/v1alpha1/` | `LLMService` and `InferenceRuntime` API types |
| `internal/controller/` | Reconcilers and controller envtest suite |
| `internal/backend/` | Shared workload builders and vendor adapters |
| `internal/gateway/` | Request admission, cold-start activation, proxying, draining, and metrics |
| `internal/model/` | Model URI resolution |
| `config/` | Kustomize deployment, generated CRDs, and generated RBAC |
| `charts/noctaya/` | Manually maintained Helm chart and synchronized CRDs |
| `examples/<vendor>/<device>/` | Independently deployable hardware profiles |
| `examples/observability/` | Optional Prometheus and Grafana integration |
| `docs/noctaya/` | Docusaurus presentation and GitHub Pages deployment |
| `test/` | Kind E2E suites and the CPU vLLM stub |

## Requirements by change type

### Go code

- Add or update the closest tests and follow the test style already used by that package.
- Preserve the Apache-2.0 header on Go files.
- Keep comments for non-obvious ownership, lifecycle, or scale-to-zero decisions; do not narrate
  code that is already clear.
- Keep reconciliation idempotent and continue treating KEDA as an optional dependency.
- Use structured controller-runtime logging and follow the surrounding message style.

Run `make test` and `make lint` for a completed Go change.

### APIs and RBAC

Edit the API types or Kubebuilder markers, not the generated output. Update the controller,
builders, tests, examples, and [CRD reference](docs/crd.md) when the behavior changes.
Then run:

```bash
make manifests generate
make helm-crds
```

Commit the source and resulting generated files together, after reviewing the generated diff for
unrelated churn.

### Backend vendors and hardware profiles

Adding a vendor normally requires all of the following:

1. A thin adapter and focused rendering tests under `internal/backend/<vendor>/`.
2. Registration in `internal/backend/registry/registry.go`.
3. The API validation enum update in `api/v1alpha1/inferenceruntime_types.go`.
4. A device-specific profile under `examples/<vendor>/<device>/`.
5. Documentation that distinguishes rendering coverage from physical validation.
6. Regenerated manifests, deepcopy code, and Helm CRDs.

Do not claim accelerator support from unit tests or manifests alone. A hardware claim must record
the device, topology, driver and device-plugin versions, runtime image, Noctaya version, commands,
and observed results. Use the [NVIDIA A10 report](docs/nvidia/a10-validation.md) and
[Ascend validation guide](docs/ascend/ascend-validation.md) as templates.

### Gateway and scaling

Gateway changes should cover the affected success, timeout, rejection, streaming, cancellation,
and drain paths. If behavior crosses KEDA activation or scale-down, run both scale-to-zero E2E
modes in addition to focused and standard tests.

### Helm, Kustomize, and images

The Helm templates are maintained separately from `config/`; keep both installation paths aligned.
RBAC, manager flags, image settings, and deployment behavior may require changes in both places.
Validate chart changes with:

```bash
helm lint charts/noctaya
helm template noctaya charts/noctaya --namespace noctaya-system >/dev/null
```

The operator and gateway use separate images. Build them with `make docker-build IMG=...` and
`make docker-build-gateway GATEWAY_IMG=...` respectively.

### Documentation and examples

Check commands, paths, versions, image names, API fields, and support claims against the source.
Keep examples independently deployable and device-specific. Prefer linking to a detailed guide
instead of duplicating long procedures.

Markdown under `docs/`, `examples/README.md`, and the root project documents is rendered directly
by the Docusaurus application under `docs/noctaya/`; do not copy it into the site directory. To
preview documentation changes, install Node.js 20 or newer and run:

```bash
cd docs/noctaya
npm ci
npm start
```

Run `npm run build` before submitting changes that affect documentation navigation, links,
styling, or the website configuration.

## Generated files

Do not hand-edit these files:

- `api/v1alpha1/zz_generated.deepcopy.go`;
- `config/crd/bases/*.yaml`;
- `config/rbac/role.yaml`;
- `charts/noctaya/crds/*.yaml`;
- `internal/gateway/externalscaler/*.pb.go`; or
- `PROJECT`.

Do not remove or relocate `+kubebuilder:scaffold:*` markers. Use the corresponding generator or
Kubebuilder command and commit generated output with its source. After changing
`externalscaler.proto`, run `go generate ./internal/gateway/externalscaler`. Note that `make deploy`
and `make build-installer` can update the manager image in `config/manager/`; check for incidental
changes afterward.

## Commits and pull requests

Create a focused branch from `main` and keep each pull request to one concern. Use a short,
imperative [Conventional Commit](https://www.conventionalcommits.org/) subject, for example:

```text
fix(gateway): preserve activation during client retry
docs: clarify Ascend validation scope
```

Every commit must include a DCO sign-off:

```bash
git commit -s
```

CI validates the Helm chart, lint, unit and envtest coverage, manager E2E, and both scale-to-zero
modes. Pull requests must pass the required checks, resolve review conversations, and receive a
code-owner approval. New commits dismiss previous approvals. Once those requirements are met, a
maintainer can enable GitHub's native auto-merge.
