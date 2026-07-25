# Ascend 910B3 deployment validation

## Status

| Profile | Validation level | Status |
|---|---|---|
| Ascend 910B3, one 64 GB device | Scale-to-zero verified | Full `0 -> 1 -> 0` lifecycle passed on physical hardware on 2026-07-15. |

The result covers the operator, Ascend Device Plugin, KEDA, gateway, model cache, and vLLM-Ascend
on the recorded stack. The server exposed one device, so multi-replica scaling was not tested.

The profile follows the official
[vLLM-Ascend installation guide](https://docs.vllm.ai/projects/ascend/en/main/installation/),
[vLLM-Ascend support guidance](https://docs.vllm.ai/projects/ascend/en/main/faqs.html), and
[Huawei Ascend Device Plugin guide](https://www.hiascend.com/document/detail/en/mindcluster/730/clustersched/schedulingug/dlug_installation_019.html).
Shared evidence requirements are in the [Ascend hardware validation guide](ascend-validation.md).

## Validation baseline

| Item | Required value |
|---|---|
| Host OS | Ubuntu |
| Device allocation | Standard non-mixed, whole-device mode |
| Kubernetes resource | `huawei.com/Ascend910` |
| Node labels | `accelerator=huawei-Ascend910`, `serving.noctaya.io/ascend-product=ascend-910b3` |
| Runtime image | `quay.io/ascend/vllm-ascend:v0.21.0rc1` |
| Smoke model | `Qwen/Qwen2.5-0.5B-Instruct` from ModelScope |
| Context limit | Explicit `--max-model-len=2048` |
| Execution baseline | `--enforce-eager` |
| Replica limit | `scaling.max: 1` for a one-device server |
| Drain timeout | `60s` |

## Ascend 910B3 result — 2026-07-15

The complete operator, device-plugin, gateway, KEDA, and vLLM path passed on this baseline:

| Item | Observed value |
|---|---|
| Accelerator | One physical Ascend `910B3`, chip version V1, 64 GB HBM |
| Host | Arm64 Ubuntu 26.04 LTS; 16-core HiSilicon TaiShan-v110; kernel `6.8.0-134-generic` |
| Firmware / driver / CANN | `7.7.0.1.231` / `26.0.rc1` / `9.0.0` |
| Kubernetes | K3s `v1.36.2+k3s1`; containerd `v2.3.2-k3s2` |
| Ascend Device Plugin | MindCluster `v7.3.0` |
| Helm / KEDA | Helm `v4.2.3`; KEDA `2.20.1` |
| vLLM-Ascend image | `v0.21.0rc1` with `vllm 0.21.0` |
| Noctaya images | Operator `0.2.0-rc.1`; gateway `0.2.0-rc.1` |
| Model / cache | `Qwen/Qwen2.5-0.5B-Instruct`; NodeLocalPVC on a dedicated 120 GB ext4 data disk |

Observed functional results:

- MindCluster reported one healthy `huawei.com/Ascend910` resource and allocated it to the backend
  Pod.
- The prewarm Job downloaded 954 MB in 66 seconds without requesting an NPU.
- A cold streaming request drove `0 -> 1`, received gateway heartbeats while the model loaded, and
  completed with real NPU tokens, `[DONE]`, and HTTP 200 in `230.99 s`.
- Warm JSON and streaming requests completed in `0.245 s` and `0.293 s`.
- KEDA returned the backend to zero and released the NPU. A cached restart reached Ready in
  `2 min 47 s`.
- Reject mode returned `503` with `Retry-After`; bounded-queue testing returned `429` after the
  100-request queue filled.
- Gateway health, queue, OpenAI-compatible inference, and Prometheus metric endpoints passed.
- A `15s` drain aborted generation, while `60s` completed 129 streaming chunks with `[DONE]` and a
  normal finish reason. The example therefore uses `drainTimeout: 60s`.
- A no-op apply, gateway replacement, operator restart, same-values Helm upgrade, and full host
  reboot preserved the expected service and cache state. Post-reboot inference again completed and
  returned to zero.

`kube-prometheus-stack`, Volcano, and HAMi were not installed during this run. Monitoring remained
optional, and no multi-replica claim is made from the one-device result.

## 1. Record the environment

Run on the NPU node and save the output:

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

Record the exact server product, NPU identifier and memory, host architecture, firmware, driver,
CANN, device-plugin, container-runtime, Kubernetes, KEDA, and image versions.

## 2. Verify the device resource

This runbook assumes Ascend Device Plugin is installed in standard non-mixed mode. The target node
must report a non-zero `huawei.com/Ascend910` value:

```bash
kubectl get nodes \
  -o custom-columns='NAME:.metadata.name,ASCEND910:.status.allocatable.huawei\.com/Ascend910'
```

Check the device-plugin Pod log and confirm that the intended device is healthy. Do not continue
until the node resource and plugin health both pass.

## 3. Label the NPU node

MindCluster's standard label identifies the Ascend 910 family. The Noctaya label restricts this
profile to the physically validated 910B3 product:

```bash
kubectl label node <npu-node> accelerator=huawei-Ascend910 --overwrite
kubectl label node <npu-node> serving.noctaya.io/ascend-product=ascend-910b3 --overwrite
kubectl get node <npu-node> -L accelerator,serving.noctaya.io/ascend-product
```

## 4. Prepare Noctaya

Use a dedicated cluster and namespace. Confirm the context, then install KEDA and Noctaya by
following [Getting started](../started.md):

```bash
kubectl config current-context
kubectl create namespace noctaya-910b-validation
```

If the cluster has no default dynamic StorageClass, set `cache.storageClassName` in the service
example before applying it. On K3s, place both the K3s data directory and local-path storage on the
data disk through `/etc/rancher/k3s/config.yaml`; do not edit generated local-path manifests.

Verify a test PVC on the data disk before downloading model weights.

## 5. Deploy the profile

```bash
SERVICE=qwen-910b-validation

kubectl apply -k examples/ascend/910b3 -n noctaya-910b-validation
kubectl get llmservice,pvc,job,deploy,pod,scaledobject \
  -n noctaya-910b-validation -w
```

Wait for the prewarm Job to complete and KEDA to hold the backend at zero. During activation,
confirm that the backend Pod lands on the labeled node and requests one device:

```bash
kubectl get pod -n noctaya-910b-validation \
  -l "serving.noctaya.io/llmservice=$SERVICE" \
  -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,NPU:.spec.containers[0].resources.limits.huawei\.com/Ascend910'
```

## 6. Exercise inference and scale-to-zero

Watch the backend Deployment:

```bash
kubectl get deployment "$SERVICE" -n noctaya-910b-validation -w
```

In another terminal, expose the gateway:

```bash
kubectl port-forward -n noctaya-910b-validation "service/$SERVICE" 8080:80
```

Send a streaming request. The gateway may emit heartbeat comments while the model loads:

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"$SERVICE\",\"stream\":true,\"max_tokens\":32,\"messages\":[{\"role\":\"user\",\"content\":\"Reply with: Noctaya 910B validation passed\"}]}"
```

Record idle replicas at zero, the cold request causing `0 -> 1`, Loading-to-Ready status, a
complete stream ending in `[DONE]`, and the backend returning to zero after the stabilization
window. Confirm with `npu-smi info` that the serving process disappears after scale-down.

To validate drain, start a response long enough to remain active, delete the backend Pod, and
confirm that the client receives `[DONE]` with a normal finish reason before termination.

## 7. Troubleshooting

### Backend remains Pending

```bash
kubectl describe pod -n noctaya-910b-validation <backend-pod>
kubectl get nodes -L accelerator
kubectl describe node <npu-node> | grep -A5 -B5 Ascend910
```

Check the advertised resource, node label, taints, device-plugin health, and cache PVC binding.

### Prewarm fails

```bash
kubectl logs -n noctaya-910b-validation job/qwen-910b-validation-prewarm
kubectl describe pvc -n noctaya-910b-validation qwen-910b-validation-cache
```

Check ModelScope egress, DNS, proxy settings, StorageClass availability, disk capacity, and runtime
image compatibility.

### Backend never becomes Ready

```bash
kubectl logs -n noctaya-910b-validation deployment/qwen-910b-validation --all-containers
kubectl describe pod -n noctaya-910b-validation \
  -l serving.noctaya.io/llmservice=qwen-910b-validation
```

Check the image, driver, firmware, CANN compatibility, driver projections, device assignment, and
rendered `vllm serve` arguments before changing probe limits.

### Driver does not load

The validated host initially had a kernel against which the `26.0.rc1` driver did not build. Use a
kernel supported by the exact server and driver combination; do not copy the validated kernel
version without checking compatibility. Verify the driver again after reboot.

### NPU out of memory

```bash
npu-smi info
kubectl get pod -n noctaya-910b-validation <backend-pod> -o yaml
```

Check for competing NPU processes and confirm the model and context limit. vLLM reserves most
available HBM for KV cache by default, so high reported use does not by itself mean that model
weights occupy that amount.

### Gateway activation timeout

Inspect prewarming, scheduling, startup, and readiness before increasing `activationTimeout`. A
longer timeout does not fix missing devices, incompatible software, failed downloads, or probe
failures.

### Stream aborts during termination

Keep `drainTimeout: 60s` as the validated baseline. A shorter value aborted generation on this
stack. Larger models or longer outputs may require a longer runtime termination grace period.

## Success criteria

The 910B3 profile passes physical validation only when:

- the node reports a healthy `huawei.com/Ascend910` resource and matches the runtime selector;
- prewarming completes without requesting an NPU;
- the backend receives one device and `/health` gates readiness correctly;
- an OpenAI-compatible streaming request completes through the gateway with `[DONE]`;
- KEDA completes an observed `0 -> 1 -> 0` loop and releases the NPU;
- an in-flight stream completes during backend termination; and
- the driver, firmware, CANN, device-plugin, image, logs, and timings are recorded.

A multi-replica claim additionally requires more than one schedulable device, multiple Ready Pods
on distinct allocations, and an observed `1 -> N -> 0` lifecycle.
