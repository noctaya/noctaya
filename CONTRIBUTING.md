# Contributing to Noctaya

Thank you for contributing. Focused fixes, tests, examples, documentation, and reproducible hardware evidence are especially valuable while Noctaya is alpha.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). Contributions are licensed under [Apache-2.0](https://github.com/noctaya/noctaya/blob/main/LICENSE) and require a [Developer Certificate of Origin](https://developercertificate.org/) (DCO) sign-off.

## Get involved

- Use [Issues](https://github.com/noctaya/noctaya/issues) for bugs and feature requests.
- Use [Discussions](https://github.com/noctaya/noctaya/discussions) for design questions and ideas.
- Look for `good first issue` or `help wanted` when choosing a task.
- Report vulnerabilities privately through the [security policy](SECURITY.md).

Read the [architecture](docs/architecture.md) and [roadmap](ROADMAP.md) before changing behavior.

## Contribution workflow

### 1. Pick or propose work

Search existing issues and pull requests before starting. Open an issue before changing an API, adding a dependency or backend vendor, moving a component boundary, or beginning a large refactor. Describe the motivation, proposed behavior, and alternatives.

### 2. Set up your environment

Install the Go version declared by [`go.mod`](go.mod), Git, and `make`. Docker is the default container tool; use `CONTAINER_TOOL=podman` for Podman. Cluster tests also require `kubectl`, Kind, and Helm.

```bash
git clone https://github.com/noctaya/noctaya.git
cd noctaya

make build
make test
make lint
```

The Makefile downloads pinned tools into `bin/`. Build and test commands may format source or regenerate files, so inspect `git diff` afterward.

### 3. Create a branch

```bash
git checkout -b feat/<short-description>
```

Keep each branch and pull request focused on one concern.

### 4. Develop and test

Use focused tests while iterating:

```bash
go test ./internal/backend/...
go test ./internal/gateway/...
go test ./internal/model/...
go test ./test/vllm-stub/...
```

Run the checks relevant to the completed change:

| Change | Checks |
|---|---|
| Go or controller behavior | `make test` and `make lint` |
| Documentation or website | `make test-docs` |
| Helm chart | `helm lint charts/noctaya` and `helm template noctaya charts/noctaya --namespace noctaya-system >/dev/null` |
| KEDA activation or scale-down | `make test-e2e` |
| Upgrade, rollback, or component recovery | `make test-release` |

The E2E runner owns an isolated Kind cluster and refuses to reuse an existing one. Never point it at development, staging, or production clusters. Most work can be tested without an accelerator; see [Developing without an accelerator](docs/no-gpu.md).

### 5. Commit your change

Use a short, imperative [Conventional Commit](https://www.conventionalcommits.org/) subject:

```text
fix(gateway): preserve activation during client retry
docs: clarify Ascend validation scope
```

Sign every commit:

```bash
git commit -s
```

Reference related issues with `Fixes #<number>` or `Refs #<number>` where appropriate.

### 6. Open a pull request

Push your branch and open a pull request against `main`.

### 7. Address review

Respond to feedback, resolve each conversation, and rerun relevant checks after rebasing or making substantial revisions. Required CI checks and approvals must pass before a maintainer merges the pull request.

### Backends and hardware evidence

A new vendor requires a thin adapter and rendering tests, registry and API-enum updates, a device-specific example, aligned documentation, and regenerated API artifacts. Rendering does not prove hardware support.

Physical validation must record the device and topology, driver and device-plugin versions,
runtime image, Noctaya version, commands, and results. Follow the
[hardware validation requirements](docs/validation/requirements.md) and use an existing
[device report](docs/validation/nvidia/a10.md) as a template.

## AI-assisted contributions

AI tools may assist your work, but you remain responsible for understanding, reviewing, and testing every change.

## Notes

Do not hand-edit:

- `api/v1alpha1/zz_generated.deepcopy.go`;
- `config/crd/bases/*.yaml` or `config/rbac/role.yaml`;
- `charts/noctaya/crds/*.yaml`;
- `internal/gateway/externalscaler/*.pb.go`; or
- `PROJECT`.

Preserve every `+kubebuilder:scaffold:*` marker and review generated diffs before submission.
