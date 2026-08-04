# Examples

Each device directory contains one deployable `InferenceRuntime` and `LLMService` pair. Use these profiles as starting points; applying a profile is not hardware-validation evidence. For the full installation and scale-to-zero walkthrough, see [Getting started](../docs/getting-started.md).

## Choose a profile

Install Noctaya and KEDA independently, then provide the driver and device plugin for the selected accelerator.

| Profile | Device resource | Required node selector |
|---|---|---|
| NVIDIA A10 | `nvidia.com/gpu` | `nvidia.com/gpu.product=NVIDIA-A10` |
| NVIDIA A100 | `nvidia.com/gpu` | `nvidia.com/gpu.product=NVIDIA-A100` |
| Atlas 300I Duo | `huawei.com/Ascend310P` | `accelerator=huawei-Ascend310P`, `serving.noctaya.io/ascend-product=atlas-300i-duo` |
| Atlas 300I Pro | `huawei.com/Ascend310P` | `accelerator=huawei-Ascend310P`, `serving.noctaya.io/ascend-product=atlas-300i-pro` |
| Ascend 910B3 | `huawei.com/Ascend910` | `accelerator=huawei-Ascend910`, `serving.noctaya.io/ascend-product=ascend-910b3` |

Inspect the live labels before applying a profile:

```bash
kubectl get nodes \
  -L nvidia.com/gpu.product,accelerator,serving.noctaya.io/ascend-product
```

All bundled profiles use `NodeLocalPVC`. Provide a default dynamic StorageClass or set `spec.cache.storageClassName` in the `LLMService`.

## Deploy a profile

```bash
kubectl create namespace ai --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n ai -k examples/nvidia/a10
```

`InferenceRuntime` is cluster-scoped; `LLMService` and generated workloads use the namespace passed to `kubectl`. The root `examples/kustomization.yaml` is intentionally empty, so `kubectl apply -k examples` cannot deploy incompatible profiles together.

## Change the model

Normally, edit only `serving_v1alpha1_llmservice.yaml`:

1. Give `metadata.name` a new, model-specific value.
2. Set `spec.model.source.uri`.
3. Update `spec.runtime.argsOverride` for model-specific engine flags.
4. Size `spec.resources`, `spec.cache`, and `spec.scaling` for the model and available hardware.

Supported model sources:

| URI | Purpose |
|---|---|
| `hf://<organization>/<model>` | Download from Hugging Face |
| `modelscope://<organization>/<model>` | Download from ModelScope |
| `oci://<registry>/<repository>@sha256:<digest>` | Stage an immutable OCI model artifact into a persistent cache |
| `pvc://<claim>[/<subpath>]` | Mount pre-staged weights read-only; use `cache.strategy: None` |

OCI references must use a digest, and the artifact root must contain the model files expected by the runtime. Use `NodeLocalPVC` or `SharedPVC`; the latter requires a StorageClass that provides `ReadWriteMany`. For a private registry, reference a `kubernetes.io/dockerconfigjson` Secret through `model.source.secretRef`. See the [CRD reference](../docs/crd.md#model-and-runtime) for delivery and cache constraints.

For example, the A10 profile can serve DeepSeek-R1-Distill-Qwen-7B with these service fields:

```yaml
metadata:
  name: deepseek-r1-distill-qwen-7b-a10
spec:
  model:
    source:
      uri: modelscope://deepseek-ai/DeepSeek-R1-Distill-Qwen-7B
  runtime:
    name: vllm-nvidia-a10
    argsOverride:
      - --max-model-len=4096
      - --gpu-memory-utilization=0.9
      - --reasoning-parser=deepseek_r1
  resources:
    accelerators: 1
    cpu: "8"
    memory: 32Gi
  cache:
    strategy: NodeLocalPVC
    size: 30Gi
    prewarm: true
```

Apply only the edited service:

```bash
kubectl apply -n ai -f examples/nvidia/a10/serving_v1alpha1_llmservice.yaml
```

Use a new service name when changing models because cache PVCs and prewarm Jobs are create-once.
Change `InferenceRuntime` only when the image, accelerator integration, scheduling, probes, or lifecycle settings must change.

## Tune the gateway

Gateway admission and compute are independent from backend accelerator resources:

```yaml
spec:
  endpoint:
    maxQueue: 100
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: "1"
        memory: 256Mi
```

`maxQueue` applies to each gateway replica. Size it for the memory cost of held requests and the amount of cold-start backpressure clients can tolerate. For optional Bearer authentication and network isolation, see [Optional traffic security](https://github.com/noctaya/noctaya/tree/main/examples/security).

## Optional observability

Noctaya does not install Prometheus or Grafana. The independent
[`observability`](https://github.com/noctaya/noctaya/tree/main/examples/observability) package contains an optional `ServiceMonitor`, alert examples, and dashboard.

## Optional traffic security

Noctaya does not manage network isolation or certificates. The independent
[`security`](https://github.com/noctaya/noctaya/tree/main/examples/security) package documents client API-key authentication and provides opt-in ingress `NetworkPolicy` profiles and KEDA-to-ExternalScaler mutual TLS configuration.
