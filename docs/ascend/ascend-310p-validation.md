# Ascend 310P deployment validation

## Status

| Profile | Validation level | Status |
|---|---|---|
| Huawei Atlas 300I Duo | Scale-to-zero verified | Full `0 -> 1 -> 2 -> 0` lifecycle passed on physical 310P3 devices on 2026-07-14. |
| Huawei Atlas 300I Pro | Rendering-tested | Physical validation is still required. |

The profiles follow the official [vLLM-Ascend 310P guide](https://docs.vllm.ai/projects/ascend/en/latest/tutorials/hardwares/310p.html),
[installation guide](https://docs.vllm.ai/projects/ascend/en/main/installation/), and
[Huawei Ascend Device Plugin guide](https://www.hiascend.com/document/detail/en/mindcluster/730/clustersched/schedulingug/dlug_installation_019.html).
Shared image, evidence, and terminology requirements are in the
[Ascend hardware validation guide](ascend-validation.md).

## Validation baseline

| Item | Required value |
|---|---|
| Host OS | Ubuntu |
| Device allocation | Standard non-mixed, whole-device mode |
| Kubernetes resource | `huawei.com/Ascend310P` |
| Runtime image | `quay.io/ascend/vllm-ascend:v0.22.1rc1-310p` |
| Smoke model | `Qwen/Qwen2.5-0.5B-Instruct` from ModelScope |
| Data type | Explicit `--dtype=float16` |
| Context limit | Explicit `--max-model-len=2048` |
| Execution baseline | `--enforce-eager` |

## Atlas 300I Duo result — 2026-07-14

The complete operator, device-plugin, gateway, KEDA, and vLLM path passed on this baseline:

| Item | Observed value |
|---|---|
| Server | Atlas 300I Duo; two `310P3` devices, about 44 GB each |
| Host | Arm64 Ubuntu 26.04 LTS; kernel `6.8.0-134-generic` |
| Driver / container CANN | `26.0.rc1` / `9.1.0-beta.1` |
| Kubernetes | K3s `v1.36.2+k3s1`; containerd `2.3.2-k3s2` |
| Ascend Device Plugin | MindCluster `v7.3.0` |
| KEDA | `2.20.1` |
| vLLM-Ascend image | `v0.22.1rc1-310p` |
| Noctaya images | Operator `0.2.0-rc.1` ; gateway `0.2.0-rc.1` |
| Model / cache | `Qwen/Qwen2.5-0.5B-Instruct`; NodeLocalPVC on a dedicated 120 GB ext4 data disk |

Observed functional results:

- A direct `torch_npu` tensor operation passed on an allocated device.
- A cold streaming request completed through the gateway in `182.04 s`; the same warm request
  completed in `0.692 s`.
- Queue demand drove `0 -> 1 -> 2 -> 0`. Both replicas became Ready on distinct devices and served
  successful requests without container restarts.
- Reject mode returned `503` with `Retry-After`; a saturated 100-request queue rejected the next
  five requests with `429`.
- A 256-token stream completed with `[DONE]` after its serving Pods were deleted, validating drain.
- Cache data, the driver, device capacity, Noctaya, KEDA, and the device plugin recovered across two
  full host reboots. A no-op apply, operator restart, gateway deletion, and Helm upgrade also
  preserved the expected resources.

## 1. Record the environment

Run on each NPU node and save the output:

```bash
uname -a
cat /etc/os-release
npu-smi info
cat /usr/local/Ascend/driver/version.info
cat /etc/ascend_install.info
```

From the Kubernetes management node:

```bash
kubectl version
kubectl get nodes -o wide
kubectl get pods -n kube-system | grep -i ascend
kubectl get crd scaledobjects.keda.sh
```

Record this evidence separately for each product:

| Evidence | Atlas 300I Duo | Atlas 300I Pro |
|---|---|---|
| Exact product and NPU identifier | | |
| NPU memory | | |
| Host architecture and Ubuntu version | | |
| Firmware, driver, and CANN versions | | |
| Ascend Device Plugin version | | |
| Container runtime and Kubernetes version | | |
| KEDA version | | |
| Runtime image digest | | |

## 2. Verify the device resource

This runbook assumes Ascend Device Plugin is installed in standard non-mixed mode. Huawei documents
`huawei.com/Ascend310P` as the resource reported in that mode.

```bash
kubectl get nodes \
  -o custom-columns='NAME:.metadata.name,ASCEND310P:.status.allocatable.huawei\.com/Ascend310P'
```

Each target node must report a non-zero value. If not, correct the driver/device-plugin installation
before installing Noctaya workloads. Mixed insertion is outside this profile because it reports
product-specific resources such as `huawei.com/Ascend310P-IPro`.

## 3. Label each product node

Huawei's standard label identifies the 310P family but does not distinguish Duo from Pro. The Noctaya
label makes each validation result attributable to the intended card.

```bash
# Atlas 300I Duo
kubectl label node <duo-node> accelerator=huawei-Ascend310P --overwrite
kubectl label node <duo-node> serving.noctaya.io/ascend-product=atlas-300i-duo --overwrite

# Atlas 300I Pro
kubectl label node <pro-node> accelerator=huawei-Ascend310P --overwrite
kubectl label node <pro-node> serving.noctaya.io/ascend-product=atlas-300i-pro --overwrite

kubectl get nodes -L accelerator,serving.noctaya.io/ascend-product
```

## 4. Prepare Noctaya

Use a dedicated cluster and namespace. Confirm the kube-context before changing cluster state.
```bash
kubectl config current-context
kubectl create namespace noctaya-310p-validation
make install
```

If the cluster has no default dynamic StorageClass, set `cache.storageClassName` in both service
examples before applying them.
For K3s, configure a separate data disk through `/etc/rancher/k3s/config.yaml`; editing the bundled
local-path manifest or ConfigMap is not persistent because K3s regenerates it:

```yaml
default-local-storage-path: /var/lib/noctaya-data/local-path
```

Restart K3s, then verify both the live `local-path-config` ConfigMap and a test PVC before deploying
model weights.

## 5. Deploy one product profile

Run each profile separately so its evidence is unambiguous. Set `PROFILE` to `duo` or `pro`:

```bash
PROFILE=duo
case "$PROFILE" in
  duo) PROFILE_DIR="310p-duo" ;;
  pro) PROFILE_DIR="310p-pro" ;;
  *) echo "PROFILE must be duo or pro" >&2; exit 1 ;;
esac
SERVICE="qwen-310p-${PROFILE}-validation"

kubectl apply -k "examples/ascend/${PROFILE_DIR}" -n noctaya-310p-validation
kubectl get llmservice,pvc,job,deploy,pod -n noctaya-310p-validation -w
```

Confirm that the Pod landed on the intended node and requested one 310P:

```bash
kubectl get pod -n noctaya-310p-validation \
  -l "serving.noctaya.io/llmservice=$SERVICE" \
  -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,NPU:.spec.containers[0].resources.limits.huawei\.com/Ascend310P'
```

## 6. Exercise inference and scale-to-zero

```bash
kubectl get deploy "$SERVICE" -n noctaya-310p-validation -w
```

Wait for KEDA to hold the backend at zero while the gateway remains available. In another terminal:

```bash
kubectl port-forward -n noctaya-310p-validation "svc/$SERVICE" 8080:80
```

Send a streaming request. The gateway may emit heartbeat comments while the model loads:

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"$SERVICE\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"Reply with: Noctaya 310P validation passed\"}]}"
```

Record idle replicas at zero, the cold request causing `0 -> 1`, Loading-to-Ready status, a complete
stream ending in `[DONE]`, and the backend returning to zero after the stabilization window. The Duo
example permits two replicas; send several concurrent streams and confirm it reaches two Ready Pods
on distinct devices before returning to zero. Keep the Pro example at one replica until its physical
device topology is recorded.

## 7. Troubleshooting

### Backend remains Pending

```bash
kubectl describe pod -n noctaya-310p-validation <backend-pod>
kubectl get nodes -L accelerator,serving.noctaya.io/ascend-product
kubectl describe node <target-node> | grep -A5 -B5 Ascend310P
```

Check the device resource, product label, taints, and cache PVC binding.

### Prewarm fails

```bash
kubectl logs -n noctaya-310p-validation job/<service-name>-prewarm
kubectl describe pvc -n noctaya-310p-validation <service-name>-cache
```

Check ModelScope egress, DNS, proxy settings, StorageClass availability, and disk capacity.

### Backend never becomes Ready

```bash
kubectl logs -n noctaya-310p-validation deploy/<service-name> --all-containers
kubectl describe pod -n noctaya-310p-validation \
  -l serving.noctaya.io/llmservice=<service-name>
```

Check image/driver/CANN compatibility, driver projections, and device assignment first.

### NPU out of memory

Do not remove `--max-model-len=2048`: the official 310P guide warns that automatic context sizing can
allocate a quadratic attention mask. Confirm the rendered arguments and NPU ownership before changing
the model or context limit.

If startup fails with a BF16 operator error, confirm that `--dtype=float16` is present. The validated
310P3 path does not support the BF16 operator selected by the smoke model's default configuration.

### Gateway activation timeout

Inspect scheduling, startup, and readiness before increasing `activationTimeout`. A longer timeout
does not fix missing devices, incompatible software, failed downloads, or probe failures.

## Success criteria

A product passes physical validation only when:

- the intended node satisfies `huawei.com/Ascend310P` and the product selector;
- prewarming completes and the backend loads the model;
- `/health` gates readiness correctly;
- an OpenAI-compatible streaming request completes through the gateway;
- KEDA completes an observed `0 -> 1 -> configured max -> 0` loop when more than one device is
  available; and
- driver, firmware, CANN, device-plugin, image digest, logs, and timings are recorded.

Products that have not met every criterion remain configuration/rendering validation targets.
