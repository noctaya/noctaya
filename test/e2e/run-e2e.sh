#!/usr/bin/env bash

set -euo pipefail

for command in "${KIND}" "${KUBECTL}" "${HELM}"; do
  command -v "${command}" >/dev/null 2>&1 || {
    echo "${command} is not installed." >&2
    exit 1
  }
done

if "${KIND}" get clusters | grep -Fxq "${E2E_KIND_CLUSTER}"; then
  echo "Refusing to reuse Kind cluster '${E2E_KIND_CLUSTER}'. Choose a new disposable name." >&2
  exit 1
fi
if [[ -e "${E2E_KUBECONFIG}" ]]; then
  echo "Refusing to overwrite E2E kubeconfig '${E2E_KUBECONFIG}'." >&2
  exit 1
fi

cluster_created=0
archive_dir="$(mktemp -d "${TMPDIR:-/tmp}/noctaya-e2e-images.XXXXXX")"
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [[ "${cluster_created}" -eq 1 ]]; then
    "${KIND}" delete cluster --name "${E2E_KIND_CLUSTER}" || true
  fi
  rm -f "${E2E_KUBECONFIG}"
  rm -rf "${archive_dir}"
  exit "${status}"
}
trap cleanup EXIT INT TERM

cluster_created=1
"${KIND}" create cluster \
  --name "${E2E_KIND_CLUSTER}" \
  --kubeconfig "${E2E_KUBECONFIG}" \
  --config test/e2e/kind.yaml \
  --wait 120s

context="$(KUBECONFIG="${E2E_KUBECONFIG}" "${KUBECTL}" config current-context)"
if [[ "${context}" != "kind-${E2E_KIND_CLUSTER}" ]]; then
  echo "Unexpected E2E context '${context}'." >&2
  exit 1
fi

KUBECONFIG="${E2E_KUBECONFIG}" "${HELM}" repo add \
  kedacore https://kedacore.github.io/charts --force-update
KUBECONFIG="${E2E_KUBECONFIG}" "${HELM}" repo update kedacore
KUBECONFIG="${E2E_KUBECONFIG}" "${HELM}" upgrade --install keda kedacore/keda \
  --version "${E2E_KEDA_VERSION}" \
  --namespace keda \
  --create-namespace \
  --wait \
  --timeout 5m

make load-e2e-images \
  CONTAINER_TOOL="${CONTAINER_TOOL}" \
  E2E_ARCHIVE_DIR="${archive_dir}" \
  E2E_KIND_CLUSTER="${E2E_KIND_CLUSTER}" \
  E2E_MANAGER_IMG="${E2E_MANAGER_IMG}" \
  E2E_GATEWAY_IMG="${E2E_GATEWAY_IMG}" \
  E2E_STUB_IMG="${E2E_STUB_IMG}"

KUBECONFIG="${E2E_KUBECONFIG}" \
  NOCTAYA_E2E_KIND_CLUSTER="${E2E_KIND_CLUSTER}" \
  NOCTAYA_E2E_KUBECTL="${KUBECTL}" \
  NOCTAYA_E2E_KUSTOMIZE="${KUSTOMIZE}" \
  NOCTAYA_E2E_MANAGER_IMAGE="${E2E_MANAGER_IMG}" \
  NOCTAYA_E2E_GATEWAY_IMAGE="${E2E_GATEWAY_IMG}" \
  NOCTAYA_E2E_STUB_IMAGE="${E2E_STUB_IMG}" \
  go test -tags=e2e ./test/e2e/ -v -ginkgo.v -timeout 20m
