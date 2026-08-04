# Troubleshoot Noctaya

Noctaya reports model availability and Kubernetes-observed activation failures on each `LLMService`. The gateway remains bounded and does not receive Kubernetes credentials, replay requests, or wait indefinitely.

## Inspect a service

Start with the service conditions:

```bash
kubectl get llmservice qwen3-8b -n ai
kubectl describe llmservice qwen3-8b -n ai
```

Then inspect the backend:

```bash
kubectl get deployment,pods -n ai -l serving.noctaya.io/llmservice=qwen3-8b
kubectl describe deployment qwen3-8b -n ai
kubectl get events -n ai --sort-by=.lastTimestamp
kubectl logs deployment/qwen3-8b -n ai -c serving
```

The `LLMService` conditions have separate responsibilities:

| Condition | Meaning |
|---|---|
| `Ready` | At least one backend replica can serve requests |
| `Degraded` | Kubernetes currently observes a hard backend activation failure |
| `AutoscalingReady` | The KEDA External Push resources are configured |

`ObservedGeneration` identifies the `LLMService` generation represented by each condition.

## Activation states

| Reason | Classification | Action |
|---|---|---|
| `Activating`, `Starting` | Normal progress | Wait for Pod creation and container startup |
| `ModelLoading` | Normal progress | Watch readiness and serving-container logs |
| `SchedulingDelayed` | Recoverable delay | Check accelerator capacity, device plugins, selectors, tolerations, scheduler, and queue |
| `ImagePullFailed` | Degraded | Check the runtime image and `imagePullSecrets` |
| `OOMKilled` | Degraded | Check memory limits, model size, and runtime settings |
| `CrashLoopBackOff`, `ContainerRestarting` | Degraded | Inspect serving-container logs and the last termination reason |
| `ProgressDeadlineExceeded` | Degraded | Inspect the Deployment rollout, Pods, and events |
| `PrewarmFailed` | Degraded | Inspect the prewarm Job and registry/storage logs; delete the failed create-once Job after correcting the cause |

Pod and Deployment changes trigger reconciliation through watches. Terminating and terminal Pods are ignored, and repeated observations of the same failure class do not rewrite status or emit duplicate events.

## Timeout versus backend cause

`activation_timeout` is a gateway outcome: the request did not reach a Ready backend within `spec.scaling.activationTimeout`. It is always bounded and does not imply a particular Kubernetes failure.

The `Degraded` and `Ready` condition reasons are controller observations. They may identify an image-pull failure, OOM, crash loop, or rollout failure while the request is still waiting. A timeout can also occur with `Degraded=False` when scheduling or model loading is merely slow.

Correct the `LLMService`, `InferenceRuntime`, cluster capacity, or external dependency that caused the failure. The controller clears `Degraded` and returns the service to `Ready` when the backend recovers; recreating the `LLMService` is unnecessary.

Noctaya does not diagnose vendor internals. Use Pod events, serving-container logs, and vendor tools for the underlying cause.

For immutable cache drift, the controller names the existing PVC or Job and requires deliberate deletion. Preserve required PVC data first. For OCI delivery, inspect both `pull-oci-model` and `prewarm` containers; the backend will not consume a `.partial` directory without its readiness marker.
