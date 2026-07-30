IMG ?= ghcr.io/noctaya/noctaya:latest
YEAR ?= $(shell date +%Y)
# VERSION is injected into manager and gateway binaries. Tagged builds use the
# nearest git tag, while local builds fall back to dev outside a git checkout.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS ?= -X main.version=$(VERSION)

ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Set to podman where Docker is unavailable.
CONTAINER_TOOL ?= docker

# Fail recipes on command or pipeline errors.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt",year=$(YEAR) paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

DOCS_DIR ?= docs/noctaya

.PHONY: test-docs
test-docs: ## Install pinned documentation dependencies and build the Docusaurus site.
	npm --prefix "$(DOCS_DIR)" ci
	npm --prefix "$(DOCS_DIR)" run build

E2E_KIND_CLUSTER ?= noctaya-test-e2e
E2E_KUBECONFIG ?= $(abspath $(LOCALBIN)/noctaya-test-e2e.kubeconfig)
E2E_KEDA_VERSION ?= 2.20.1
E2E_MANAGER_IMG ?= noctaya.io/noctaya:e2e
E2E_GATEWAY_IMG ?= noctaya.io/noctaya-gateway:e2e
E2E_STUB_IMG ?= noctaya.io/vllm-stub:e2e
E2E_ARCHIVE_DIR ?= $(abspath $(LOCALBIN)/e2e-images)

.PHONY: load-e2e-images
load-e2e-images:
	rm -rf "$(E2E_ARCHIVE_DIR)"
	mkdir -p "$(E2E_ARCHIVE_DIR)"
	$(CONTAINER_TOOL) build --build-arg VERSION=$(VERSION) -t $(E2E_MANAGER_IMG) .
	$(CONTAINER_TOOL) build --build-arg VERSION=$(VERSION) -f Dockerfile.gateway -t $(E2E_GATEWAY_IMG) .
	$(CONTAINER_TOOL) build -f test/vllm-stub/Dockerfile -t $(E2E_STUB_IMG) .
	$(CONTAINER_TOOL) save $(E2E_MANAGER_IMG) -o "$(E2E_ARCHIVE_DIR)/manager.tar"
	$(KIND) load image-archive "$(E2E_ARCHIVE_DIR)/manager.tar" --name $(E2E_KIND_CLUSTER)
	$(CONTAINER_TOOL) save $(E2E_GATEWAY_IMG) -o "$(E2E_ARCHIVE_DIR)/gateway.tar"
	$(KIND) load image-archive "$(E2E_ARCHIVE_DIR)/gateway.tar" --name $(E2E_KIND_CLUSTER)
	$(CONTAINER_TOOL) save $(E2E_STUB_IMG) -o "$(E2E_ARCHIVE_DIR)/stub.tar"
	$(KIND) load image-archive "$(E2E_ARCHIVE_DIR)/stub.tar" --name $(E2E_KIND_CLUSTER)
	rm -rf "$(E2E_ARCHIVE_DIR)"

.PHONY: test-e2e
test-e2e: kustomize ## Run the External Push lifecycle in an isolated disposable Kind cluster.
	@CONTAINER_TOOL="$(CONTAINER_TOOL)" \
		E2E_GATEWAY_IMG="$(E2E_GATEWAY_IMG)" \
		E2E_KEDA_VERSION="$(E2E_KEDA_VERSION)" \
		E2E_KIND_CLUSTER="$(E2E_KIND_CLUSTER)" \
		E2E_KUBECONFIG="$(E2E_KUBECONFIG)" \
		E2E_MANAGER_IMG="$(E2E_MANAGER_IMG)" \
		E2E_STUB_IMG="$(E2E_STUB_IMG)" \
		HELM="$(HELM)" \
		KIND="$(KIND)" \
		KUBECTL="$(KUBECTL)" \
		KUSTOMIZE="$(abspath $(KUSTOMIZE))" \
		bash test/e2e/run-e2e.sh

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager and gateway binaries.
	go build -ldflags "$(LDFLAGS)" -o bin/manager cmd/main.go
	go build -ldflags "$(LDFLAGS)" -o bin/gateway ./cmd/gateway

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build --build-arg VERSION=$(VERSION) -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

GATEWAY_IMG ?= ghcr.io/noctaya/noctaya-gateway:latest
GATEWAY_IMAGE_PLACEHOLDER ?= ghcr.io/noctaya/noctaya-gateway:latest

.PHONY: docker-build-gateway
docker-build-gateway: ## Build the data-plane gateway image.
	$(CONTAINER_TOOL) build --build-arg VERSION=$(VERSION) -f Dockerfile.gateway -t ${GATEWAY_IMG} .

.PHONY: docker-push-gateway
docker-push-gateway: ## Push the data-plane gateway image.
	$(CONTAINER_TOOL) push ${GATEWAY_IMG}

STUB_IMG ?= noctaya.io/vllm-stub:e2e

.PHONY: docker-build-stub
docker-build-stub: ## Build the CPU vllm-stub image used by the no-GPU e2e harness.
	$(CONTAINER_TOOL) build -f test/vllm-stub/Dockerfile -t ${STUB_IMG} .

.PHONY: helm-crds
helm-crds: manifests ## Sync generated CRDs into the Helm chart's crds/ directory.
	cp config/crd/bases/*.yaml charts/noctaya/crds/

# docker-buildx builds and pushes every listed platform.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	- $(CONTAINER_TOOL) buildx create --name noctaya-builder
	$(CONTAINER_TOOL) buildx use noctaya-builder
	$(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --build-arg VERSION=$(VERSION) --tag ${IMG} .
	- $(CONTAINER_TOOL) buildx rm noctaya-builder

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | sed \
		's|$(GATEWAY_IMAGE_PLACEHOLDER)|$(GATEWAY_IMG)|g' > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	"$(KUSTOMIZE)" build config/crd | "$(KUBECTL)" apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/crd | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | sed \
		's|$(GATEWAY_IMAGE_PLACEHOLDER)|$(GATEWAY_IMG)|g' | "$(KUBECTL)" apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
HELM ?= helm
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.8.1
CONTROLLER_TOOLS_VERSION ?= v0.21.0

# Derived from the controller-runtime module version.
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v")

# Derived from the Kubernetes API module version.
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.12.2
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))
	@test -f .custom-gcl.yml && { \
		echo "Building custom golangci-lint with plugins..." && \
		$(GOLANGCI_LINT) custom --destination $(LOCALBIN) --name golangci-lint-custom && \
		mv -f $(LOCALBIN)/golangci-lint-custom $(GOLANGCI_LINT); \
	} || true

# Install and version-pin a local Go tool: target, package, version.
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef
