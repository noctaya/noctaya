# Getting started

This guide installs Noctaya and KEDA separately, deploys one accelerator profile, and follows a model from zero through activation and back to zero. The checked-in A10 profile uses `max: 1`; with additional capacity and sustained demand, the same lifecycle extends to `0 → 1 → N → 0`.
Choose another profile from [Examples](../examples/README.md) when needed.

## 1. Prepare the cluster

You need:

- Kubernetes 1.30 or newer and Helm 3 or newer;
- `kubectl` configured for the intended cluster;
- a compatible accelerator driver and device plugin;
- a default dynamic StorageClass, or an explicit `cache.storageClassName`;
- enough storage for the model; and
- access to the runtime image registry and model source.

Noctaya does not install device software, KEDA, serving engines, schedulers, or monitoring. Confirm the target cluster before making changes:

```bash
kubectl config current-context
kubectl get nodes
kubectl get storageclass
```

## 2. Install Noctaya

Install the chart from a local checkout:

```bash
git clone https://github.com/noctaya/noctaya.git
cd noctaya

helm upgrade --install noctaya \
  ./charts/noctaya \
  --namespace noctaya-system \
  --create-namespace
```

Verify the operator and API:

```bash
kubectl rollout status deployment/noctaya-controller-manager -n noctaya-system
kubectl get crd inferenceruntimes.serving.noctaya.io llmservices.serving.noctaya.io
```

The chart runs two leader-elected operator replicas by default, spreads them across nodes when possible, and creates a `minAvailable: 1` PodDisruptionBudget. Use `--set operator.replicas=1` only when operator high availability is unnecessary.

## 3. Install KEDA independently

Install KEDA using its official
[2.20 deployment guide](https://keda.sh/docs/2.20/deploy/) or another supported method. Noctaya does not install, upgrade, or uninstall it.

KEDA must expose its API before you create an `LLMService`:

```bash
kubectl get crd scaledobjects.keda.sh
```

Noctaya uses KEDA External Push for immediate cold activation. The chart defaults to one gateway; set `gateway.replicas` above one when gateway availability is required. Noctaya then creates a per-service demand aggregator, preferred hostname anti-affinity, and a `minAvailable: 1` PodDisruptionBudget. See [Scaling and failure behavior](architecture.md#scaling-and-failure-behavior).

For example, add `--set gateway.replicas=2` to the Noctaya Helm installation command to run two gateways per `LLMService`.

## 4. Select a profile

The A10 profile requires:

- the `nvidia.com/gpu` device resource;
- the node label `nvidia.com/gpu.product=NVIDIA-A10`;
- at least 30 GiB of dynamic storage; and
- access to ModelScope.

Confirm the node label:

```bash
kubectl get nodes -L nvidia.com/gpu.product
```

Keep the checked-in model settings for the first run. To use another accelerator or model, follow the [profile and model configuration guide](../examples/README.md).

## 5. Deploy the model

```bash
kubectl create namespace ai --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n ai -k examples/nvidia/a10

kubectl get inferenceruntime vllm-nvidia-a10
kubectl get llmservice qwen2-5-7b-a10 -n ai -o wide
```

`InferenceRuntime` is cluster-scoped. `LLMService` and its backend, gateway, cache, prewarm, and autoscaling resources are namespaced.

## 6. Observe prewarming and the idle state

Watch the generated resources:

```bash
kubectl get llmservice,deployment,pod,pvc,job,scaledobject \
  -n ai -w
```

The A10 profile creates a prewarm Job that downloads weights without requesting a GPU:

```bash
kubectl logs -f job/qwen2-5-7b-a10-prewarm -n ai
```

After reconciliation, confirm that the backend is at zero while the gateway remains available:

```bash
kubectl get llmservice qwen2-5-7b-a10 -n ai
kubectl get deployment qwen2-5-7b-a10 qwen2-5-7b-a10-gateway -n ai
```

Expected state:

- `LLMService` phase: `ScaledToZero`;
- backend Deployment: `0` replicas; and
- gateway Deployment: `1` replica.

If Noctaya was installed with `--set gateway.replicas=2`, the gateway Deployment has two replicas and an additional `qwen2-5-7b-a10-scaler` Deployment aggregates their demand.

## 7. Send a request

Keep the resource watch running. In another terminal, forward the gateway:

```bash
kubectl port-forward service/qwen2-5-7b-a10 8000:80 -n ai
```

Send a streaming request from a third terminal:

```bash
curl -N http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  --data-binary @- <<'JSON'
{
  "model": "qwen2-5-7b-a10",
  "messages": [{"role": "user", "content": "Reply with one short sentence."}],
  "stream": true
}
JSON
```

The request should move the backend from `0` to `1` and the service from `ScaledToZero` through
`Loading` to `Ready`. A fast transition may hide an intermediate phase. In `keepalive` mode,
streaming clients receive SSE comments while the backend starts.

Cold-start time depends on scheduling, image locality, cache state, model size, and hardware.

## 8. Observe the return to zero

```bash
kubectl get llmservice,deployment -n ai -w
```

After demand and the scale-down stabilization window expire, the backend should return to `0` and the service to `ScaledToZero`. The gateway remains ready for the next request.

## Troubleshooting

```bash
kubectl describe llmservice qwen2-5-7b-a10 -n ai
kubectl get events -n ai --sort-by=.lastTimestamp
kubectl logs deployment/qwen2-5-7b-a10-gateway -n ai
kubectl logs deployment/qwen2-5-7b-a10 -n ai
kubectl logs deployment/noctaya-controller-manager -n noctaya-system
```

If prewarming is enabled, also inspect `job/qwen2-5-7b-a10-prewarm`. For API defaults and unsupported alpha fields, see the [CRD reference](crd.md).

## Clean up

Delete the profile before uninstalling the operator:

```bash
kubectl delete -n ai -k examples/nvidia/a10
helm uninstall noctaya --namespace noctaya-system
```

The profile includes a cluster-scoped `InferenceRuntime`; confirm that no other service uses it.
Helm retains CRDs, and uninstalling Noctaya does not uninstall independently managed KEDA.
