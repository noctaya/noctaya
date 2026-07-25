# Examples

## Device profiles

Each directory is an independently deployable `InferenceRuntime` and `LLMService` pair for one
accelerator model. Apply only the profile that matches the devices advertised by your Kubernetes
cluster. The root `examples/kustomization.yaml` is intentionally empty so that
`kubectl apply -k examples` cannot deploy incompatible workloads across multiple accelerator
families.

Install Noctaya, KEDA, and the device plugin for the selected accelerator before applying a
profile. For example:

```bash
kubectl create namespace ai
kubectl apply -k examples/ascend/310p-duo -n ai
```

`InferenceRuntime` is cluster-scoped; `LLMService` is created in the namespace selected by `-n`.
All bundled service profiles use `NodeLocalPVC`. Ensure the cluster has a default dynamic
StorageClass, or set `cache.storageClassName` before applying a profile.

Every runtime profile selects its intended accelerator nodes:

| Profile | Device resource | Required node selector |
|---|---|---|
| NVIDIA A10 | `nvidia.com/gpu` | `nvidia.com/gpu.product=NVIDIA-A10` |
| NVIDIA A100 | `nvidia.com/gpu` | `nvidia.com/gpu.product=NVIDIA-A100` |
| Atlas 300I Duo | `huawei.com/Ascend310P` | `accelerator=huawei-Ascend310P`, `serving.noctaya.io/ascend-product=atlas-300i-duo` |
| Atlas 300I Pro | `huawei.com/Ascend310P` | `accelerator=huawei-Ascend310P`, `serving.noctaya.io/ascend-product=atlas-300i-pro` |
| Ascend 910B3 | `huawei.com/Ascend910` | `accelerator=huawei-Ascend910`, `serving.noctaya.io/ascend-product=ascend-910b3` |

Inspect the live node labels before applying a profile:

```bash
kubectl get nodes \
  -L nvidia.com/gpu.product,accelerator,serving.noctaya.io/ascend-product
```

## Change the model

Each device profile contains one `serving_v1alpha1_inferenceruntime.yaml` and one
`serving_v1alpha1_llmservice.yaml`. To serve another model on the same engine and accelerator,
normally edit only the `LLMService` manifest:

- give `metadata.name` a new, model-specific value;
- set `spec.model.source.uri` to an `hf://`, `modelscope://`, or `pvc://` source;
- update `spec.runtime.argsOverride` for model-specific engine flags; and
- size `spec.resources`, `spec.cache`, and `spec.scaling` for the model and available hardware.

For example, these are the relevant A10 fields for a DeepSeek-R1 distilled model:

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

Apply the edited service without reapplying the runtime:

```bash
kubectl apply -n ai -f examples/nvidia/a10/serving_v1alpha1_llmservice.yaml
```

## Optional observability

Noctaya exposes metrics but does not create Prometheus or Grafana resources. The independent
[`observability`](https://github.com/noctaya/noctaya/tree/main/examples/observability)
package contains an opt-in `ServiceMonitor` and dashboard.
