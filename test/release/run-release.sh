#!/usr/bin/env bash

set -euo pipefail

for command in "${CONTAINER_TOOL}" "${KIND}" "${KUBECTL}" "${HELM}"; do
  command -v "${command}" >/dev/null 2>&1 || {
    echo "${command} is not installed." >&2
    exit 1
  }
done

if "${KIND}" get clusters | grep -Fxq "${RELEASE_KIND_CLUSTER}"; then
  echo "Refusing to reuse Kind cluster '${RELEASE_KIND_CLUSTER}'. Choose a new disposable name." >&2
  exit 1
fi
if [[ -e "${RELEASE_KUBECONFIG}" ]]; then
  echo "Refusing to overwrite release kubeconfig '${RELEASE_KUBECONFIG}'." >&2
  exit 1
fi

rm -rf "${RELEASE_EVIDENCE_DIR}"
mkdir -p "${RELEASE_EVIDENCE_DIR}"
exec 19>"${RELEASE_EVIDENCE_DIR}/commands.log"
BASH_XTRACEFD=19
PS4='+ ${BASH_SOURCE##*/}:${LINENO}: '
set -x

cluster_created=0
archive_dir="$(mktemp -d "${TMPDIR:-/tmp}/noctaya-release-images.XXXXXX")"
collect_evidence() {
  {
    echo "started_at=${started_at}"
    echo "finished_at=$(date --utc +%Y-%m-%dT%H:%M:%SZ)"
    echo "previous_version=${RELEASE_PREVIOUS_VERSION}"
    echo "previous_chart=${RELEASE_PREVIOUS_CHART}"
    echo "candidate_chart=${RELEASE_CANDIDATE_CHART}"
    echo "candidate_operator_image=${E2E_MANAGER_IMG}"
    echo "candidate_gateway_image=${E2E_GATEWAY_IMG}"
    echo "stub_image=${E2E_STUB_IMG}"
    echo "keda_chart_version=${E2E_KEDA_VERSION}"
    "${KIND}" version 2>/dev/null || true
    if [[ "${cluster_created}" -eq 1 ]]; then
      KUBECONFIG="${RELEASE_KUBECONFIG}" "${KUBECTL}" version -o yaml 2>/dev/null || true
      KUBECONFIG="${RELEASE_KUBECONFIG}" "${HELM}" list --all-namespaces 2>/dev/null || true
      KUBECONFIG="${RELEASE_KUBECONFIG}" "${HELM}" history noctaya \
        --namespace noctaya-system 2>/dev/null || true
      KUBECONFIG="${RELEASE_KUBECONFIG}" "${KUBECTL}" get nodes,pods,poddisruptionbudgets \
        --all-namespaces -o wide 2>/dev/null || true
      KUBECONFIG="${RELEASE_KUBECONFIG}" "${KUBECTL}" get \
        inferenceruntimes,llmservices,scaledobjects --all-namespaces -o yaml \
        >"${RELEASE_EVIDENCE_DIR}/resources.yaml" 2>/dev/null || true
    fi
  } >"${RELEASE_EVIDENCE_DIR}/environment.txt"
}
cleanup() {
  status=$?
  trap - EXIT INT TERM
  collect_evidence
  if [[ "${cluster_created}" -eq 1 ]]; then
    "${KIND}" delete cluster --name "${RELEASE_KIND_CLUSTER}" || true
  fi
  rm -f "${RELEASE_KUBECONFIG}"
  rm -rf "${archive_dir}"
  exit "${status}"
}
trap cleanup EXIT INT TERM

started_at="$(date --utc +%Y-%m-%dT%H:%M:%SZ)"
cluster_created=1
"${KIND}" create cluster \
  --name "${RELEASE_KIND_CLUSTER}" \
  --kubeconfig "${RELEASE_KUBECONFIG}" \
  --config test/e2e/kind.yaml \
  --wait 120s

context="$(KUBECONFIG="${RELEASE_KUBECONFIG}" "${KUBECTL}" config current-context)"
if [[ "${context}" != "kind-${RELEASE_KIND_CLUSTER}" ]]; then
  echo "Unexpected release-validation context '${context}'." >&2
  exit 1
fi

KUBECONFIG="${RELEASE_KUBECONFIG}" "${HELM}" repo add \
  kedacore https://kedacore.github.io/charts --force-update
KUBECONFIG="${RELEASE_KUBECONFIG}" "${HELM}" repo update kedacore
KUBECONFIG="${RELEASE_KUBECONFIG}" "${HELM}" upgrade --install keda kedacore/keda \
  --version "${E2E_KEDA_VERSION}" \
  --namespace keda \
  --create-namespace \
  --wait \
  --timeout 5m

make load-e2e-images \
  CONTAINER_TOOL="${CONTAINER_TOOL}" \
  E2E_ARCHIVE_DIR="${archive_dir}" \
  E2E_KIND_CLUSTER="${RELEASE_KIND_CLUSTER}" \
  E2E_MANAGER_IMG="${E2E_MANAGER_IMG}" \
  E2E_GATEWAY_IMG="${E2E_GATEWAY_IMG}" \
  E2E_STUB_IMG="${E2E_STUB_IMG}"

KUBECONFIG="${RELEASE_KUBECONFIG}" "${HELM}" upgrade --install noctaya \
  "${RELEASE_PREVIOUS_CHART}" \
  --namespace noctaya-system \
  --create-namespace \
  --set operator.replicas=1 \
  --set operator.leaderElect=true \
  --set gateway.replicas=1 \
  --set gateway.scalerMode=external-push \
  --wait \
  --timeout 5m

KUBECONFIG="${RELEASE_KUBECONFIG}" \
  NOCTAYA_RELEASE_CANDIDATE_CHART="${RELEASE_CANDIDATE_CHART}" \
  NOCTAYA_RELEASE_CONTAINER_TOOL="${CONTAINER_TOOL}" \
  NOCTAYA_RELEASE_GATEWAY_IMAGE="${E2E_GATEWAY_IMG}" \
  NOCTAYA_RELEASE_HELM="${HELM}" \
  NOCTAYA_RELEASE_KIND="${KIND}" \
  NOCTAYA_RELEASE_KIND_CLUSTER="${RELEASE_KIND_CLUSTER}" \
  NOCTAYA_RELEASE_KUBECTL="${KUBECTL}" \
  NOCTAYA_RELEASE_MANAGER_IMAGE="${E2E_MANAGER_IMG}" \
  NOCTAYA_RELEASE_PREVIOUS_VERSION="${RELEASE_PREVIOUS_VERSION}" \
  NOCTAYA_RELEASE_STUB_IMAGE="${E2E_STUB_IMG}" \
  go test -tags=release ./test/release/ -v -ginkgo.v -timeout 25m \
  2>&1 | tee "${RELEASE_EVIDENCE_DIR}/test.log"
