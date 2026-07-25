# NVIDIA A10 deployment validation

## Status

| Profile | Validation level | Status |
|---|---|---|
| NVIDIA A10 | Scale-to-zero and coexistence verified | Noctaya v0.3.0-rc.1 passed `0 -> 1 -> 2 -> 0` on two physical 24 GB GPUs on 2026-07-19. |

The current result covers the operator, gateway, NVIDIA device plugin, KEDA external-push scaler,
Volcano scheduling, model cache, vLLM, and concurrent operation with a Kthena-managed hot model.
Noctaya recovered automatically after a real host reboot. A separate Kthena recovery qualification
is recorded below and does not change the Noctaya result. The run follows the official
[NVIDIA Container Toolkit installation guide](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html),
[K3s NVIDIA runtime guidance](https://docs.k3s.io/advanced#nvidia-container-runtime-support),
[NVIDIA device-plugin documentation](https://github.com/NVIDIA/k8s-device-plugin), and
[vLLM OpenAI-compatible server documentation](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/).

The hardware-neutral [Noctaya and Kthena walkthrough](../demo.md) shows the core lifecycle
with real `kubectl` and `curl` commands.

## Validation baseline

| Item | Required value |
|---|---|
| Device allocation | Whole-GPU mode; no MIG or time-slicing |
| Kubernetes resource | `nvidia.com/gpu` |
| Node selector | `nvidia.com/gpu.product=NVIDIA-A10` |
| Runtime image | `vllm/vllm-openai:v0.25.1` |
| Smoke model | `Qwen/Qwen2.5-7B-Instruct` from ModelScope |
| Context limit | Explicit `--max-model-len=4096` |
| GPU memory target | `--gpu-memory-utilization=0.9` |
| Autoscaling | KEDA `2.20.1`, external-push mode |
| Scheduling | Volcano `1.15.0`, with separate queues for Noctaya and Kthena |
| Replica limit | `scaling.max: 1` normally; temporarily raised to `2` for the two-GPU test |
| Drain timeout | `120s` |
| Cache | 30 GiB `NodeLocalPVC` with prewarming |

## Physical result — 2026-07-19

| Item | Observed value |
|---|---|
| Accelerator | Two physical NVIDIA A10 GPUs; 23,028 MiB each; PHB topology; no NVLink |
| Host | x86-64 Ubuntu 22.04; 32 vCPU; 125 GiB RAM; kernel `5.15.0-125-generic` |
| Driver | NVIDIA `550.90.07` |
| Kubernetes | K3s `v1.36.2+k3s1` |
| Helm / KEDA | Helm `4.2.3`; KEDA `2.20.1` |
| Volcano / Kthena | Volcano `1.15.0`; Kthena `1.0.0` |
| NVIDIA integration | Container Toolkit `1.19.1`; device plugin `0.19.3`; whole-GPU mode |
| vLLM image | Private-registry mirror of `vllm/vllm-openai:v0.25.1`; immutable digest was not retained |
| Noctaya images | Locally published operator and gateway builds from `v0.3.0-rc.1`; immutable digests were not retained |
| Model / cache | `Qwen/Qwen2.5-7B-Instruct`; 30 GiB NodeLocalPVC on a dedicated 120 GB ext4 data disk |

The checked-in profile deliberately remains scheduler-neutral and uses the public vLLM image. The
integration run used internal image mirrors, selected Volcano with a `noctaya-longtail` queue, set a
queue target of one for deterministic scale-out, and temporarily raised `spec.scaling.max` to `2`.
The model, positional vLLM argument, accelerator request, resources, probes, lifecycle, cache, and
metrics matched the checked-in profile. Volcano is not baked into the base profile because it is an
optional cluster integration.

Observed functional results:

- The device plugin advertised exactly two `nvidia.com/gpu` resources. A one-GPU CUDA matrix
  operation passed in the supplied vLLM image.
- The accelerator-free prewarm Job completed in `6m43s`. The cache used about 15 GB across 18 files
  and retained fingerprint
  `b69e9c0308766121393edf2a8b924f5c6f9bbc6d444e910ce9694ad6d615959b` after reboot.
- A cold request drove `0 -> 1` through the external-push scaler, received SSE heartbeats, and
  completed with real model output and HTTP 200 in `91.876s`.
- Six pending requests drove `1 -> 2`; Volcano scheduled two Ready Pods on distinct physical GPU
  UUIDs. Direct inference through both Pods returned HTTP 200, followed by observed `2 -> 1 -> 0`.
- Kthena served a hot model while Noctaya used the other GPU. Both generated 512 tokens concurrently
  in about `16.6s`; both requests returned HTTP 200 and both GPUs reached 100% sampled utilization.
- A 105-client cold burst produced exactly 100 admitted HTTP 200 heartbeat streams and five HTTP
  429 responses. Gateway counters matched the observed results.
- Reject mode returned HTTP 503 with `Retry-After: 10`, while the activation lease kept external
  demand active. A later warm request returned HTTP 200.
- A streaming request completed with HTTP 200 and `[DONE]` after its exact backend Pod was deleted;
  the configured pre-stop drain protected the in-flight stream.
- vLLM exposed `vllm:kv_cache_usage_perc`, queue, running-request, and time-to-first-token metrics.
- Same-values Helm upgrade and replacement of the Noctaya controller, gateway, KEDA operator,
  Volcano scheduler, Kthena controller, and NVIDIA device plugin all converged with new Pod UIDs.
- After a real host reboot, the data disk remounted, the cache fingerprint was unchanged, GPU
  capacity returned to two, and Noctaya recovered automatically in `ScaledToZero` with its gateway
  Ready.
- A fractional accelerator request was rejected at render time with an explicit unsupported error
  and created no owned workload, which is the expected boundary for this whole-GPU profile.

### Kthena reboot-recovery qualification

During the host reboot, the Kthena serving Pod was admitted before the NVIDIA device plugin had
healthy devices and ended in `Completed` after an `UnexpectedAdmissionError`. Kthena did not
recreate that terminal Pod during an observation period longer than two minutes, despite its
`ServingGroupRecreate` recovery policy. Deleting the failed Pod manually caused immediate
recreation with a new UID, and the route then returned HTTP 200.

Noctaya recovered without manual intervention. Treat this as a Kthena/device-plugin startup-order
recovery gap and test it independently before relying on the combined stack for unattended reboot
recovery.

### Validation boundaries

- HAMi, fractional GPU sharing, MIG, multi-node scheduling, and gang scheduling were not validated.
- `kube-prometheus-stack` was not installed; metrics endpoints were inspected directly.
- GPU Feature Discovery could not be pulled from `registry.k8s.io` in this environment. The exact
  A10 label was applied manually only after hardware identity was verified with `nvidia-smi`.
- The Kthena router's K3s LoadBalancer helper remained Pending because Traefik occupied the required
  host ports. Routing was verified through the in-cluster Service.
- Exact image tags were recorded, but immutable image digests were not retained. Pin and record
  digests when repeating the run for supply-chain or release provenance.

## 1. Record the environment

Run on each target node and retain the output:

```bash
uname -a
cat /etc/os-release
nvidia-smi
nvidia-container-cli --version
```

From the Kubernetes management node:

```bash
kubectl version
kubectl get nodes -o wide
kubectl get crd scaledobjects.keda.sh
helm list --all-namespaces
```

Record the GPU product, count, memory, UUIDs, compute capability, driver, Container Toolkit,
device-plugin, container-runtime, Kubernetes, KEDA, and image versions.

## 2. Configure the K3s NVIDIA runtime

Install the Container Toolkit using NVIDIA's distribution-specific instructions. K3s discovers the
NVIDIA runtime when its executable is on the service's path. Configure it as the default runtime in
`/etc/rancher/k3s/config.yaml`, then restart K3s:

```yaml
default-runtime: nvidia
```

```bash
systemctl restart k3s
kubectl get node
```

If the runtime cannot find `runc`, point the toolkit at the K3s-bundled executable rather than
installing a second untracked runtime. Verify the rendered K3s containerd configuration and run a
disposable CUDA Pod before continuing.

## 3. Install and verify the device plugin

Install an explicitly pinned official chart in whole-GPU mode:

```bash
helm repo add nvdp https://nvidia.github.io/k8s-device-plugin --force-update
helm upgrade --install nvidia-device-plugin nvdp/nvidia-device-plugin \
  --version 0.19.3 \
  --namespace nvidia-device-plugin \
  --create-namespace \
  --set runtimeClassName=nvidia \
  --set migStrategy=none \
  --set failOnInitError=true
```

Confirm the DaemonSet is Ready and that capacity matches the physical whole GPUs:

```bash
kubectl rollout status daemonset/nvidia-device-plugin -n nvidia-device-plugin
kubectl get nodes \
  -o custom-columns='NAME:.metadata.name,GPUS:.status.allocatable.nvidia\.com/gpu'
```

The profile requires `nvidia.com/gpu.product=NVIDIA-A10`. GPU Feature Discovery or equivalent
platform automation should publish it. In a focused lab without discovery, add it manually only
after confirming the exact product with `nvidia-smi`:

```bash
kubectl label node <a10-node> nvidia.com/gpu.present=true --overwrite
kubectl label node <a10-node> nvidia.com/gpu.product=NVIDIA-A10 --overwrite
kubectl get node <a10-node> -L nvidia.com/gpu.product
```

## 4. Place K3s data on the data disk

Keep container images and model PVCs off a small system disk. For K3s, configure both locations in
`/etc/rancher/k3s/config.yaml` after mounting the data disk persistently:

```yaml
data-dir: /var/lib/noctaya-data/k3s
default-local-storage-path: /var/lib/noctaya-data/local-path
```

Restart K3s and prove the placement with a disposable PVC. Do not rely only on the live local-path
ConfigMap because K3s regenerates packaged manifests.

## 5. Deploy the A10 profile

Install Noctaya and KEDA first. For the default Kubernetes scheduler, apply the independent profile
to a dedicated namespace:

```bash
kubectl create namespace noctaya-a10-validation
kubectl apply -k examples/nvidia/a10 -n noctaya-a10-validation
kubectl get inferenceruntime vllm-nvidia-a10
kubectl get llmservice,pvc,job,deploy,pod,scaledobject \
  -n noctaya-a10-validation -w
```

The base profile deliberately does not require Volcano. To repeat the optional Volcano path,
configure the runtime before creating the `LLMService`; the prewarm Job is immutable and inherits
the scheduler only when it is first created:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: noctaya-longtail
spec:
  parent: root
  weight: 1
  reclaimable: true
```

```bash
kubectl create namespace noctaya-a10-validation
kubectl apply -f queue.yaml
kubectl apply -f \
  examples/nvidia/a10/serving_v1alpha1_inferenceruntime.yaml
kubectl patch inferenceruntime vllm-nvidia-a10 --type merge \
  -p '{"spec":{"accelerator":{"scheduler":{"name":"volcano","queue":"noctaya-longtail"}}}}'
kubectl apply -n noctaya-a10-validation -f \
  examples/nvidia/a10/serving_v1alpha1_llmservice.yaml
```

This is an alternative to `kubectl apply -k examples/nvidia/a10`, not a second step after it.
Kthena workloads and queues are installed and managed independently; see the
[operational walkthrough](../demo.md) for the coexistence boundary.

On a lab with two otherwise-idle A10 GPUs, raise the maximum only for the scale-out test:

```bash
kubectl patch llmservice qwen2-5-7b-a10 -n noctaya-a10-validation \
  --type merge -p '{"spec":{"scaling":{"max":2}}}'
```

Wait for prewarming to complete and the backend to settle at zero. Confirm that the Job did not
request a GPU and the backend template requests one:

```bash
kubectl get job qwen2-5-7b-a10-prewarm -n noctaya-a10-validation
kubectl get deployment qwen2-5-7b-a10 -n noctaya-a10-validation -o yaml
```

## 6. Exercise inference and scale-to-zero

Expose the stable gateway Service:

```bash
kubectl port-forward -n noctaya-a10-validation service/qwen2-5-7b-a10 8080:80
```

Send a streaming request while the backend is at zero:

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"qwen2-5-7b-a10","stream":true,"messages":[{"role":"user","content":"Reply with: Noctaya A10 validation passed"}]}'
```

Record gateway heartbeats, `0 -> 1`, load-gated readiness, HTTP 200, real model tokens, `[DONE]`,
and the eventual return to zero. For two-device validation, hold more than ten concurrent requests
and prove that two Ready Pods have different allocated GPU UUIDs before allowing `2 -> 1 -> 0`.

Check the runtime metrics directly while a backend exists:

```bash
kubectl port-forward -n noctaya-a10-validation service/qwen2-5-7b-a10-backend 8000:8000
curl -fsS http://127.0.0.1:8000/metrics | \
  grep -E 'vllm:(kv_cache_usage_perc|num_requests_waiting|num_requests_running|time_to_first_token_seconds)'
```

## 7. Recovery and lifecycle checks

For a complete validation, retain object UIDs and cache fingerprints before each test, then verify:

- no-op profile reapply;
- operator and gateway Pod replacement;
- same-values Noctaya Helm upgrade;
- KEDA and optional scheduler/controller replacement;
- device-plugin Pod replacement and a fresh CUDA smoke Pod;
- in-flight streaming completion during backend Pod deletion; and
- full host reboot with persistent data-disk mount, cache identity, GPU capacity, and post-reboot
  inference.

If another serving system shares the cluster, verify its route and recovery independently before
and after disruptive tests. A successful Noctaya recovery does not establish another controller's
recovery behavior.

Do not run destructive recovery tests on a shared or production cluster.

## Troubleshooting

### Backend remains Pending

Check the exact product label, allocatable `nvidia.com/gpu`, taints, PVC binding, and competing GPU
workloads:

```bash
kubectl describe pod -n noctaya-a10-validation <backend-pod>
kubectl get nodes -L nvidia.com/gpu.product
kubectl describe node <a10-node>
nvidia-smi
```

### Device plugin is Ready but reports no GPUs

Verify the host driver first, then confirm K3s recognized the NVIDIA runtime and the plugin uses the
expected runtime class. A healthy host `nvidia-smi` does not by itself prove that containerd can
inject a GPU.

### Backend never becomes Ready

```bash
BACKEND_POD=$(kubectl get pod -n noctaya-a10-validation \
  -l serving.noctaya.io/llmservice=qwen2-5-7b-a10 \
  --field-selector=status.phase=Running \
  -o jsonpath='{.items[0].metadata.name}')
kubectl logs -n noctaya-a10-validation "$BACKEND_POD"
kubectl describe pod -n noctaya-a10-validation \
  -l serving.noctaya.io/llmservice=qwen2-5-7b-a10
```

Check image/driver compatibility, rendered arguments, cache contents, GPU assignment, and startup
probe history. The A10 profile uses the positional model argument required by current `vllm serve`.

### Gateway activation timeout

Inspect prewarming, scheduling, startup, and readiness before increasing `activationTimeout`. A
longer timeout does not repair missing devices, an incompatible image, or failed model loading.

### Kthena Pod stays terminal after reboot

Wait until the NVIDIA device plugin is Ready and the node advertises its expected allocatable GPU
count. Then inspect the Pod event before deciding whether to replace it:

```bash
kubectl get node \
  -o custom-columns='NAME:.metadata.name,GPUS:.status.allocatable.nvidia\.com/gpu'
kubectl describe pod -n <kthena-workload-namespace> <serving-pod>
```

In the recorded run, deleting the terminal serving Pod let Kthena recreate it after the device
plugin recovered. This is a Kthena/device-plugin recovery workaround, not Noctaya reconciliation.

## Success criteria

The A10 profile passes physical validation only when:

- each target node advertises healthy `nvidia.com/gpu` capacity and matches the exact A10 selector;
- prewarming completes without requesting a GPU;
- the backend receives one whole GPU and `/health` gates readiness correctly;
- cold and warm OpenAI-compatible streaming requests complete through the gateway with `[DONE]`;
- KEDA completes an observed `0 -> 1 -> configured max -> 0` loop and releases the GPUs;
- external-push activation is claimed only after the ScaledObject becomes active from real gateway
  demand and returns inactive after scale-down;
- multiple replicas use distinct physical allocations when `max` is greater than one;
- an in-flight stream completes during backend termination; and
- the driver, toolkit, device plugin, exact image references, cache identity, logs, and timings are
  recorded; retain immutable image digests when release provenance requires reproducibility beyond
  a mutable registry tag.

Kthena coexistence is a separate integration claim. It additionally requires concurrent successful
inference through both systems, distinct whole-GPU allocations, independent controller recovery,
and explicit recording of any reboot qualification.
