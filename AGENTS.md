# Noctaya Agent Guide

## Scope and sources of truth

Noctaya owns the Kubernetes orchestration and lifecycle layer for scale-to-zero LLM serving:
declarative workloads, runtime selection, model loading and caching, health, accelerator scheduling translation, autoscaling integration, and stable metrics.

Do not move inference kernels, vendor runtime behavior, device plugins, schedulers, monitoring lifecycle, fleet routing, or datacenter serving into Noctaya. Vendor adapters must remain thin Kubernetes translations.

The API group is `serving.noctaya.io/v1alpha1`:

- `LLMService` is namespaced and describes serving intent.
- `InferenceRuntime` is cluster-scoped and describes a reusable runtime and accelerator profile.
Its controller is passive; the `LLMService` reconciler consumes it.

Read the relevant source before editing:

- `docs/architecture.md` defines the project boundary and lifecycle.
- `CONTRIBUTING.md` defines the human workflow.
- API types and generated CRDs define schemas.
- The Makefile and `.github/workflows/` define commands and CI.
- Physical-validation reports define hardware claims.

Keep tutorials in the appropriate document under `docs/`, not in this guide.

## Repository map

| Path | Responsibility |
|---|---|
| `cmd/` | Operator and gateway entry points |
| `api/v1alpha1/` | CRD types and Kubebuilder markers |
| `internal/controller/llmservice/` | Serving lifecycle reconciler and envtest coverage |
| `internal/controller/inferenceruntime/` | Passive runtime controller |
| `internal/backend/runtime/` | Runtime contracts and shared vLLM rendering |
| `internal/backend/resources/` | Kubernetes resource builders and rendering tests |
| `internal/backend/{ascend,nvidia,registry}/` | Thin vendor adapters and built-in registration |
| `internal/gateway/{proxy,scaler,demand}/` | Admission, routing, demand reporting, and External Push scaling |
| `internal/model/` | Model URI resolution |
| `config/` and `charts/noctaya/` | Kustomize and manually maintained Helm packaging |
| `examples/` | Device profiles and optional integrations |
| `docs/validation/` | Shared validation requirements and physical-device reports |
| `docs/noctaya/` | Docusaurus presentation for `noctaya.io` |
| `test/e2e/` | Disposable Kind External Push lifecycle |
| `test/vllm-stub/` | CPU-only vLLM protocol stub |

There is one API group and no webhook. Do not move Kubebuilder-owned files or scaffold APIs or webhooks unless explicitly requested.
When scaffolding is required, use Kubebuilder and preserve every `+kubebuilder:scaffold:*` marker.

## Work safely

Inspect `git status` before editing. Existing changes belong to the user: preserve unrelated work, avoid overlapping broad rewrites, and never discard changes to clean the tree. Do not commit unless the user requests it.

Never hand-edit:

- `api/v1alpha1/zz_generated.deepcopy.go`;
- `config/crd/bases/*.yaml` or `config/rbac/role.yaml`;
- `charts/noctaya/crds/*.yaml`;
- `internal/gateway/externalscaler/*.pb.go`; or
- `PROJECT`.

After changing API types, validation/default markers, scope, or RBAC markers, run:

```bash
make manifests generate
make helm-crds
```

After changing `externalscaler.proto`, run:

```bash
go generate ./internal/gateway/externalscaler
```

Review generated diffs and reject unrelated churn. Helm templates are not generated from `config/`; align RBAC, manager settings, images, and CRDs across both installation paths. `make deploy` and `make build-installer` may change the manager image under `config/manager/`.

## Architecture invariants

One `LLMService` normally owns a backend Deployment and Service, gateway Deployment and public Service, optional cache and prewarm resources, a KEDA `ScaledObject`, and an internal ExternalScaler Service. Multiple gateways also use one aggregate-scaler Deployment.

Preserve these rules:

- Reconciliation is idempotent.
- Mutable owned resources use server-side apply with field owner `noctaya-operator` and controller references.
- Cache PVCs and prewarm Jobs are create-once because they contain immutable fields.
- KEDA is required for scaling but installed independently. Never bundle it with the chart or import its Go SDK; its CRD must exist before an `LLMService` is deployed.
- Monitoring remains outside reconciliation; optional resources belong in `examples/observability/`.
- Watch owned resources through controller-runtime instead of periodic requeues.
- Update status only when changed, use `metav1.Condition`, and set `ObservedGeneration`.
- Backend builders never set replicas; KEDA owns the backend `0..N` count.
- Gateway and backend replicas remain separate. Multiple gateways publish sequenced, expiring demand reports to the per-service aggregate scaler; do not bypass that path.
- Vendor behavior stays behind `internal/backend/runtime.BackendAdapter`; shared vLLM behavior stays common.
- Keep external CRDs unstructured unless a typed dependency is an explicit design decision.

When adding a vendor, add and test the adapter, register it, update the API enum, add a device-specific example, update documentation, and regenerate API artifacts. Rendering is not hardware evidence: claims must record the physical device, topology, driver and device plugin, runtime image, Noctaya version, commands, and results.

## Route changes

### Go, APIs, and controllers

Update the closest tests and follow the package's existing `testing` or Ginkgo/Gomega style. Use Kubernetes API conventions, preserve backward compatibility where practical, and keep controllers, builders, examples, tests, generated files, and `docs/crd.md` aligned. `InferenceRuntime` remains cluster-scoped. Run `make test` and `make lint`.

### Gateway and scaling

Cover affected success, timeout, rejection, streaming, cancellation, and drain paths. If behavior crosses KEDA activation or scale-down, run the External Push E2E lifecycle.

### Packaging and documentation

Keep chart values, templates, Kustomize manifests, installation docs, and release behavior aligned.
Canonical Markdown stays in `docs/`, `examples/README.md`, and root project files; `docs/noctaya/` owns presentation, not duplicate content. Keep hardware examples independently deployable and verify every documented command, version, image, API field, and support claim.

### Releases

1. Update the chart `version` and `appVersion`.
2. Move changelog entries into a dated release and retain an empty `[Unreleased]` section.
3. Update current install/status references without rewriting historical validation evidence.
4. Reconcile hardware claims with validation reports.
5. Validate the chart and inspect `.github/workflows/release.yml` before tagging.

## Verification

Use the Go version in `go.mod`. Prefer the closest package test while iterating; controller tests require envtest, so use `make test` unless `KUBEBUILDER_ASSETS` is configured.

| Command | Purpose |
|---|---|
| `make build` | Generate, format, vet, and build; may modify files |
| `make test` | Generate, format, vet, run unit/envtest coverage, and write `cover.out` |
| `make lint` | Run the pinned linter |
| `make lint-fix` | Apply supported fixes; inspect every edit |
| `make test-docs` | Install pinned Node dependencies and build Docusaurus |
| `make test-e2e` | Run the External Push lifecycle on an isolated Kind cluster |

E2E commands may target only the disposable Kind cluster owned by the runner. Confirm the kube-context and never target development, staging, or production.

## Style and completion

- Preserve Apache-2.0 headers and `serving.noctaya.io/*` and `app.kubernetes.io/*` contracts.
- Use compact lowercase Go package names and lowercase snake_case Go filenames by responsibility. Use lowercase kebab-case for manually maintained non-Go paths unless an ecosystem tool or established contract defines the name.
- Prefer focused changes; avoid unrelated refactors, dependencies, and generated churn.
- Comment only non-obvious ownership, lifecycle, Kubernetes, or scale-to-zero decisions.
- Use structured logging with balanced key/value pairs.
- Log messages start with a capital letter, have no final punctuation, name the action, and use past tense for failures, for example `"Failed to create Deployment"`.
- When a commit is requested, use a focused Conventional Commit subject and `git commit -s`.

Before declaring completion, inspect the diff, run proportionate checks, and report exactly what ran and what did not.
