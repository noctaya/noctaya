# Observability

Noctaya exposes Prometheus metrics from both the always-on gateway and the selected backend runtime.

The generated gateway and backend Services expose a named `http` port and carry both of these
stable discovery labels:

```text
app.kubernetes.io/managed-by=noctaya
serving.noctaya.io/llmservice=<llmservice-name>
```

## Install kube-prometheus-stack (optional)

The Noctaya profile uses the `monitoring.coreos.com/v1` `ServiceMonitor` CRD. If the cluster does not
already provide that CRD and a compatible Prometheus instance, install
[`kube-prometheus-stack`](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack).
The stack also includes Grafana.

The chart selects only its own `ServiceMonitor` labels by default. Save this minimal override as
`monitoring-values.yaml` so Prometheus can discover the independent Noctaya profile across workload
namespaces:

```yaml
prometheus:
  prometheusSpec:
    serviceMonitorSelectorNilUsesHelmValues: false
    serviceMonitorNamespaceSelector: {}
```

```bash
helm upgrade --install kube-prometheus-stack \
  oci://ghcr.io/prometheus-community/charts/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f monitoring-values.yaml
```

An empty namespace selector discovers `ServiceMonitor` objects in every namespace. In a shared or
multi-tenant cluster, replace it with a label selector for only the namespaces Prometheus may
scrape.

## Apply the Noctaya ServiceMonitor

The independent
[`examples/observability/prometheus`](https://github.com/noctaya/noctaya/tree/main/examples/observability/prometheus)
profile selects both Services in one workload namespace and scrapes `/metrics` every 15 seconds.
Apply the profile once in each namespace containing `LLMService` objects:

```bash
kubectl apply -k examples/observability/prometheus -n ai
```

The Prometheus instance must discover `ServiceMonitor` objects in that namespace and accept the
profile's labels. The example matches the bundled runtimes, which expose metrics at `/metrics`;
customize it for a runtime that uses a different path. Noctaya serving and autoscaling continue
normally when either the monitoring stack or this profile is absent.

## Import the Grafana dashboard

1. Open Grafana and go to **Dashboards** > **New** > **Import**.
2. Upload or paste
   [`examples/observability/grafana/noctaya-overview.json`](https://github.com/noctaya/noctaya/blob/main/examples/observability/grafana/noctaya-overview.json).
3. Select the Prometheus data source for the dashboard's `DS_PROMETHEUS` input.
4. Import the dashboard.

The dashboard expects Prometheus to scrape Noctaya gateway metrics and the backend vLLM `/metrics`
endpoint through the optional discovery configuration above.

## Upgrade cleanup from v0.1.0

Noctaya v0.1.0 created one controller-owned `ServiceMonitor` per `LLMService`. Those resources retain
owner references and disappear when their service is deleted, but upgrading Noctaya does not
otherwise remove them. Delete them once when adopting the independent profile to avoid duplicate
scrape targets:

```bash
kubectl delete servicemonitors.monitoring.coreos.com -A \
  -l 'app.kubernetes.io/managed-by=noctaya,serving.noctaya.io/llmservice'
```

## Gateway metrics

The gateway exports these metrics on `/metrics`:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `noctaya_gateway_pending` | Gauge | none | Requests admitted and waiting or in flight. |
| `noctaya_gateway_demand` | Gauge | none | Effective queue value reported to KEDA, including the activation-lease floor. |
| `noctaya_gateway_requests_total` | Counter | `code` | Responses by HTTP status code. |
| `noctaya_gateway_rejections_total` | Counter | `reason` | Rejected requests by reason. |
| `noctaya_gateway_activation_wait_seconds` | Histogram | none | Time spent holding a request until the backend was ready. |
| `noctaya_gateway_scaler_streams` | Gauge | none | Connected KEDA external-push activation streams. |
| `noctaya_gateway_activation_events_total` | Counter | none | Inactive-to-active effective-demand transitions. |

`noctaya_gateway_rejections_total` uses reasons such as `queue_full`, `cold_start`, and
`activation_timeout`. The activation wait histogram is exposed with the standard Prometheus
`_bucket`, `_sum`, and `_count` series.
