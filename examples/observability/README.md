# Optional observability

Noctaya exposes metrics but does not install or manage Prometheus or Grafana. This directory contains an optional `ServiceMonitor`, alert examples, and Grafana dashboard; serving and autoscaling work without them.

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

## Add alert examples

Apply the `PrometheusRule` separately:

```bash
kubectl apply -k examples/observability/alerts -n ai
```

The rules cover persistent queue saturation, repeated activation timeouts, high activation wait, and a missing KEDA External Push stream. They are conservative examples, not SLOs. Review the thresholds, `for` durations, labels, and routing before using them in production. The rules require the `llmservice` target label added by the supplied `ServiceMonitor`.

## Import the dashboard

In Grafana, import `grafana/noctaya-overview.json` and select the Prometheus data source for `DS_PROMETHEUS`.

## Gateway metrics

| Metric | Type | Meaning |
|---|---|---|
| `noctaya_gateway_pending` | Gauge | Admitted requests waiting or in flight |
| `noctaya_gateway_demand` | Gauge | Local effective demand, including the activation lease |
| `noctaya_gateway_requests_total` | Counter | Responses by HTTP status code |
| `noctaya_gateway_rejections_total` | Counter | Rejections by reason |
| `noctaya_gateway_activation_wait_seconds` | Histogram | Time spent waiting for backend readiness |
| `noctaya_gateway_scaler_streams` | Gauge | Connected External Push scaler streams |
| `noctaya_gateway_activation_events_total` | Counter | Inactive-to-active demand transitions |
| `noctaya_gateway_demand_reports_total` | Counter | Multi-gateway demand publication results |

With multiple gateways, the aggregate-scaler Service also exposes:

| Metric | Type | Meaning |
|---|---|---|
| `noctaya_scaler_demand` | Gauge | Aggregate demand reported to KEDA |
| `noctaya_scaler_gateway_members` | Gauge | Gateway members with unexpired reports |
| `noctaya_scaler_demand_reports_total` | Counter | Processed demand reports by result |
| `noctaya_scaler_expired_members_total` | Counter | Members removed after report expiry |

Backend metrics depend on the selected `InferenceRuntime`.

## Activation failures

Metrics describe gateway traffic and demand. Kubernetes-observed backend failures are reported through `LLMService` conditions, events, and controller logs; Noctaya does not add an unbounded model, Pod, or error-message label to Prometheus metrics.

See [Troubleshoot Noctaya](../../docs/troubleshooting.md) for failure classes and recovery commands. An `activation_timeout` gateway rejection is a bounded request outcome and may occur without a hard backend failure when scheduling or model loading is slow.
