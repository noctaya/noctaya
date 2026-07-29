# Optional traffic security

Noctaya does not manage cluster networking or certificates. These opt-in profiles isolate Noctaya-managed Pods and secure KEDA's External Push connection with credentials supplied by the cluster operator.

For multiple gateway replicas, Noctaya automatically creates a per-`LLMService` Secret that authenticates gateway demand reports to the aggregate scaler. NetworkPolicy remains necessary to restrict which workloads can reach that internal endpoint.

## NetworkPolicy

The base profile permits only:

- labeled client namespaces to gateway HTTP on `8080`;
- Noctaya gateways to model backends on the named `http` port;
- Noctaya gateways to a multi-gateway aggregate scaler on `9091`; and
- the KEDA operator in namespace `keda` to ExternalScaler gRPC on `9090`.

Before applying it:

1. Confirm the cluster network plugin enforces Kubernetes `NetworkPolicy`.
2. Change the KEDA namespace or operator Pod selector if the installation differs.
3. Keep the runtime container port named `http`, or change the backend policies.
4. Label each trusted client or ingress-controller namespace:

   ```bash
   kubectl label namespace <client-namespace> serving.noctaya.io/client-access=true
   ```

Apply the ingress-only profile in each `LLMService` namespace:

```bash
kubectl apply -n ai -k examples/security/network-policy
```

This profile does not isolate egress. If the cluster also applies egress-deny policies, separately allow DNS, the Kubernetes API where required, model and image registries, object storage, gateways to backends, and gateways to the aggregate scaler.

To permit the optional Prometheus profile, label the Prometheus namespace and apply the monitoring overlay instead:

```bash
kubectl label namespace monitoring serving.noctaya.io/monitoring-access=true
kubectl apply -n ai -k examples/security/network-policy/monitoring
```

Gateway and backend metrics share their serving ports. Apply this overlay only to a trusted monitoring namespace because it also permits access to the gateway API and backend HTTP endpoint.

Review namespace labels, Pod selectors, ingress-controller behavior, and tenant boundaries before production use.

## ExternalScaler mutual TLS

Mutual TLS is optional. Noctaya mounts an existing server Secret and references an existing KEDA authentication object; it does not issue or rotate certificates.

Prepare two Secrets in the `LLMService` namespace:

| Secret | Required keys | Purpose |
|---|---|---|
| `noctaya-external-scaler-server` | `tls.crt`, `tls.key`, `ca.crt` | ExternalScaler server identity and the CA that verifies KEDA clients |
| `noctaya-external-scaler-client` | `tls.crt`, `tls.key`, `ca.crt` | KEDA client identity and the CA that verifies the ExternalScaler |

The server certificate must include `<llmservice>-scaler.<namespace>.svc` as a DNS SAN. The client certificate must permit client authentication. The same CA may sign both identities, but separate CAs are also supported.

Apply the supplied `TriggerAuthentication`:

```bash
kubectl apply -n ai -k examples/security/mtls
```

Then add this to the `LLMService`:

```yaml
spec:
  scaling:
    externalScaler:
      tls:
        serverSecretName: noctaya-external-scaler-server
        authenticationRef:
          name: noctaya-external-scaler-mtls
          kind: TriggerAuthentication
```

For centrally managed credentials, use a KEDA `ClusterTriggerAuthentication` and set `kind: ClusterTriggerAuthentication`. Noctaya requires all three server files and fails closed if any are missing or invalid.

After rotating the server Secret, restart `<llmservice>-gateway` for a single gateway or `<llmservice>-scaler` for multiple gateways so the ExternalScaler reloads its files. Coordinate the client-side rotation with KEDA before removing the previous trust chain.
