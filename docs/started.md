# Getting started

This guide installs Noctaya, deploys one hardware profile, and sends a request through the
scale-to-zero gateway. For contributor workflows, see
[CONTRIBUTING.md](../CONTRIBUTING.md). For a complete loop without an accelerator, see
[Developing without a GPU](no-gpu.md).

## Prerequisites

Before deploying a model, provide:

- Kubernetes 1.29 or newer for Noctaya, with `kubectl` pointing to the intended cluster;
- Helm;
- KEDA for autoscaling and scale-to-zero—the linked KEDA 2.20 release requires Kubernetes 1.30 or
  newer;
- a driver and device plugin compatible with the selected accelerator;
- enough storage for the selected model, or a pre-staged `pvc://` model source; and
- registry and model-source access required by the selected profile.

Noctaya does not install hardware drivers, device plugins, KEDA, model-serving engines, or
schedulers. Check the target before continuing:

```bash
kubectl config current-context
kubectl get nodes
```

Use a dedicated development cluster while evaluating Noctaya.

## Install Noctaya

When autoscaling or scale-to-zero is required, install KEDA first by following the official
[KEDA 2.20 deployment guide](https://keda.sh/docs/2.20/deploy/). Then install the chart from the
repository:

```bash
git clone https://github.com/noctaya/noctaya.git
cd noctaya

helm upgrade --install noctaya \
  ./charts/noctaya \
  --namespace noctaya-system \
  --create-namespace

kubectl rollout status deployment/noctaya-controller-manager -n noctaya-system
kubectl get crd inferenceruntimes.serving.noctaya.io llmservices.serving.noctaya.io
```

KEDA is optional to the reconciler: without its CRD, Noctaya still creates the serving resources but
skips the `ScaledObject`. Autoscaling and scale-to-zero are then disabled.

The chart defaults to KEDA's polling `metrics-api` scaler. To push cold activation immediately,
enable the ExternalScaler transport in the release chart:

```bash
helm upgrade --install noctaya \
  ./charts/noctaya \
  --namespace noctaya-system \
  --create-namespace \
  --set gateway.scalerMode=external-push \
  --set gateway.replicas=1
```

External-push is an operator-wide setting and currently requires exactly one gateway replica. Set
`gateway.scalerMode=metrics-api` to roll back without changing any `LLMService` objects. See
[Scaler transport](architecture.md#scaler-transport) for the lifecycle and security details.

## Select one hardware profile

Each directory under [`examples/<vendor>/<device>/`](../examples/README.md) contains an independently
deployable `InferenceRuntime` and `LLMService` pair. Apply only a profile matching the extended
resource advertised by the installed device plugin. The validation matrix in
[`examples/README.md`](../examples/README.md) distinguishes physical validation from rendering-only
coverage.

For example, the NVIDIA A10 profile expects `nvidia.com/gpu`, the exact
`nvidia.com/gpu.product=NVIDIA-A10` node label, a default dynamic StorageClass with at least 30 GiB
available, and outbound access to ModelScope. From the current source checkout, apply it with:

```bash
kubectl create namespace ai --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n ai -k examples/nvidia/a10

kubectl get inferenceruntime vllm-nvidia-a10
kubectl get llmservice -n ai
```

The A10 profile uses vLLM `v0.25.1` and its positional model argument. Its whole-device lifecycle
was physically validated through `0→1→2→0`; see the
[A10 validation report](nvidia/a10-validation.md). GPU Feature Discovery or equivalent platform
automation should publish the exact product label. In a focused lab, apply it manually only after
confirming the hardware identity with `nvidia-smi`.

`InferenceRuntime` is cluster-scoped. `LLMService` and all generated workloads are created in the
`ai` namespace. If several equal-priority runtimes for the same vendor are installed, pin
`spec.runtime.name`; Noctaya deliberately rejects an ambiguous vendor-only selection.

All bundled profiles use `NodeLocalPVC`. If the cluster has no default StorageClass, set
`cache.storageClassName` to a dynamic StorageClass before applying the profile.

## Understand the LLMService

The A10 profile includes the following workload shape. It pins the runtime so deployment does not
depend on vendor-selection priority:

```yaml
apiVersion: serving.noctaya.io/v1alpha1
kind: LLMService
metadata:
  name: qwen2-5-7b-a10
spec:
  model:
    source:
      uri: modelscope://Qwen/Qwen2.5-7B-Instruct
  runtime:
    name: vllm-nvidia-a10
    argsOverride:
      - --max-model-len=4096
      - --gpu-memory-utilization=0.9
  resources:
    accelerators: 1
    cpu: "8"
    memory: 32Gi
  scaling:
    min: 0
    max: 1
    metric: queueDepth
    target: 10
    activationTimeout: 5m
  cache:
    strategy: NodeLocalPVC
    size: 30Gi
    prewarm: true
  endpoint:
    openAICompatible: true
    coldStart:
      mode: keepalive
      heartbeatInterval: 10s
```

The important relationships are:

- `runtime.name` selects a cluster-scoped runtime profile;
- `resources.accelerators` is the number of whole devices requested by each backend replica;
- `scaling.min: 0` allows KEDA to release all accelerators while idle;
- `scaling.max` bounds backend replicas, not gateway replicas;
- `cache.prewarm` downloads weights without consuming an accelerator; and
- the always-on gateway exposes the OpenAI-compatible endpoint and KEDA demand signal.

The checked-in `scaling.max: 1` is a safe default. Raise it only after confirming the cluster has
additional free A10 GPUs; each backend replica requests one whole `nvidia.com/gpu` resource.

The same API shape can target Ascend by pinning the matching Ascend runtime and using a compatible
model and runtime configuration. This is API portability, not a claim that every model, image, or
runtime flag is interchangeable between devices.

## Observe prewarming and activation

Watch the resources created for the service:

```bash
kubectl get llmservice,deployment,pod,service,pvc,job,scaledobject -n ai -w
```

The prewarm Job hydrates `qwen2-5-7b-a10-cache`. When no request is pending, KEDA can hold the backend
Deployment at zero while the gateway remains available. Inspect failures with:

```bash
kubectl describe llmservice qwen2-5-7b-a10 -n ai
kubectl logs job/qwen2-5-7b-a10-prewarm -n ai
kubectl get events -n ai --sort-by=.lastTimestamp
```

## Send a request

Forward the gateway Service from one terminal:

```bash
kubectl port-forward service/qwen2-5-7b-a10 8000:80 -n ai
```

Then send a streaming request from another terminal:

```bash
curl -N http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "qwen2-5-7b-a10",
    "messages": [{"role": "user", "content": "Reply with one short sentence."}],
    "stream": true
  }'
```

The first request raises the gateway queue signal and activates the backend. In `keepalive` mode,
SSE comment heartbeats can appear while the model starts. Depending on model size, image locality,
cache state, and hardware, activation can take seconds to minutes; it is not a 60-second guarantee.

After the configured stabilization window and when no requests remain, KEDA scales the backend
back to zero.

## Upgrade considerations

Upgrade with the next versioned chart asset:

```bash
NOCTAYA_VERSION=<new-version>
helm upgrade noctaya \
  "https://github.com/noctaya/noctaya/releases/download/v${NOCTAYA_VERSION}/noctaya-${NOCTAYA_VERSION}.tgz" \
  --namespace noctaya-system
```

If the CRDs were previously managed with `kubectl apply` or `make install`, inspect their field
ownership before moving them under Helm. Helm 4 uses server-side apply for CRDs and can report
conflicts with an existing manager. Back up custom resources and test the migration; do not delete
CRDs containing live `LLMService` or `InferenceRuntime` objects merely to clear ownership.

Cache PVCs and prewarm Jobs contain immutable fields and are created once. Changing the model or
cache configuration may require intentionally replacing those resources; see
[Caching](architecture.md#caching).

Kubernetes does not interpret a renamed example runtime or service as an in-place update. When an
alpha release changes example identities, deploy and validate the new service, preserve any cache
data you need, then remove the legacy service and remove the old cluster-scoped runtime only after
confirming nothing uses it. Review the [changelog](../CHANGELOG.md) before upgrading.

## Clean up

Delete the service profile before removing the operator. The profile also contains a cluster-scoped
runtime, so confirm that no other service uses it:

```bash
kubectl delete -n ai -k examples/nvidia/a10
helm uninstall noctaya --namespace noctaya-system
```

Helm does not remove CRDs from a chart's `crds/` directory during uninstall. Keep them when custom
resources remain, and remove them only as an explicit cluster-administration decision.
