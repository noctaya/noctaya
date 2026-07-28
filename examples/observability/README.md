# Optional observability

Noctaya exposes metrics but does not install or manage Prometheus or Grafana. This directory contains an optional `ServiceMonitor` and Grafana dashboard; serving and autoscaling work without them.

## Prerequisites

With kube-prometheus-stack, allow Prometheus to discover independently managed `ServiceMonitor` objects:

```yaml
prometheus:
  prometheusSpec:
    serviceMonitorSelectorNilUsesHelmValues: false
    serviceMonitorNamespaceSelector: {}
```

An empty namespace selector permits discovery in every namespace. Use a restricted selector in shared or multi-tenant clusters.

## Collect metrics

Apply one `ServiceMonitor` in each namespace containing `LLMService` objects:

```bash
kubectl apply -k examples/observability/prometheus -n ai
```

The profile selects Noctaya-managed Services and scrapes their named `http` port at `/metrics` every 15 seconds. Adjust the path for runtimes that expose metrics elsewhere.

## Import the dashboard

In Grafana, import `grafana/noctaya-overview.json` and select the Prometheus data source for `DS_PROMETHEUS`.

## Gateway metrics

| Metric | Type | Meaning |
|---|---|---|
| `noctaya_gateway_pending` | Gauge | Admitted requests waiting or in flight |
| `noctaya_gateway_demand` | Gauge | Queue demand reported to KEDA, including the activation lease |
| `noctaya_gateway_requests_total` | Counter | Responses by HTTP status code |
| `noctaya_gateway_rejections_total` | Counter | Rejections by reason |
| `noctaya_gateway_activation_wait_seconds` | Histogram | Time spent waiting for backend readiness |
| `noctaya_gateway_scaler_streams` | Gauge | Connected External Push scaler streams |
| `noctaya_gateway_activation_events_total` | Counter | Inactive-to-active demand transitions |

Backend metrics depend on the selected `InferenceRuntime`.
