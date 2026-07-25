# Noctaya and Kthena operational walkthrough

This hardware-neutral walkthrough shows two serving policies sharing one Kubernetes cluster:

https://github.com/user-attachments/assets/2d217dad-0280-4509-8793-dfd13ce0cdfa

- Kthena keeps a frequently used model ready for low-latency traffic.
- Noctaya holds a long-tail model at zero replicas until a request arrives.
- KEDA's external-push scaler activates the Noctaya backend while the gateway keeps the client
  connection alive with SSE heartbeats.
- Volcano schedules both workloads without making the scheduler part of Noctaya itself.

## What the walkthrough demonstrates

1. The Kthena-managed hot model is Ready while the Noctaya `LLMService` is `ScaledToZero`.
2. The Kthena route returns a real OpenAI-compatible response.
3. A request to the Noctaya gateway creates demand and receives heartbeats during model startup.
4. KEDA activates the backend, and Kubernetes reports the backend Pod becoming Ready.
5. The Noctaya route returns a real OpenAI-compatible response.
6. When demand ends, the Noctaya backend returns to zero while the Kthena model remains Running.

## Observe the same lifecycle

The commands below assume Noctaya, KEDA, Volcano, Kthena, a vendor device plugin, and compatible
runtime profiles are already installed. Kthena remains an independent platform; Noctaya does not
install or reconcile its resources.

Set the names used by your environment in each shell:

```bash
export NAMESPACE=ai-serving
export LONGTAIL_MODEL=qwen-longtail
export HOT_MODEL=qwen-hot
```

Confirm the initial placement and lifecycle state:

```bash
kubectl get llmservice "$LONGTAIL_MODEL" -n "$NAMESPACE" \
  -o custom-columns=NAME:.metadata.name,PHASE:.status.phase
kubectl get modelserving "$HOT_MODEL" -n "$NAMESPACE"
kubectl get podgroup -n "$NAMESPACE"
kubectl get deployment,pod -n "$NAMESPACE"
```

To exercise the hot-model route, forward the independently installed Kthena router in one terminal:

```bash
kubectl port-forward -n kthena-system service/kthena-router 8081:80
```

Then send a request from another terminal:

```bash
curl -sS http://127.0.0.1:8081/v1/chat/completions \
  -H 'Content-Type: application/json' \
  --data-binary @- <<JSON
{"model":"$HOT_MODEL","messages":[{"role":"user","content":"Reply with: Kthena hot pass"}]}
JSON
```

In one terminal, watch Noctaya activation:

```bash
kubectl get deployment "$LONGTAIL_MODEL" -n "$NAMESPACE" -w
```

In another terminal, expose the stable Noctaya gateway:

```bash
kubectl port-forward -n "$NAMESPACE" "service/$LONGTAIL_MODEL" 8080:80
```

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  --data-binary @- <<JSON
{"model":"$LONGTAIL_MODEL","stream":true,"messages":[{"role":"user","content":"Reply with: Noctaya long-tail pass"}]}
JSON
```

Inspect KEDA while the request is pending, then wait for the backend to return to zero:

```bash
kubectl get scaledobject "$LONGTAIL_MODEL" -n "$NAMESPACE"
kubectl get hpa "keda-hpa-$LONGTAIL_MODEL" -n "$NAMESPACE"
kubectl get deployment "$LONGTAIL_MODEL" -n "$NAMESPACE" -w
```

External-push mode requires one gateway replica until Noctaya supports demand aggregation across
gateway Pods. The reference scenario uses whole-device scheduling and does not claim HAMi, MIG,
fractional accelerators, multi-node topology, or production readiness.
