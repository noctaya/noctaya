# Security Policy

Noctaya is alpha software that reconciles Kubernetes workloads and proxies inference traffic.
Security reports may affect cluster permissions, model or request data, generated workloads, or
published release artifacts. Please report suspected vulnerabilities privately.

## Supported versions

Noctaya does not maintain release branches or backport fixes during the alpha phase. Fixes land on
`main` and are included in the next release; users should upgrade to the newest published release.

| Channel | Support |
|---|---|
| Latest published release | Supported |
| `main` | Fixes land here first; development code, not a stable release |
| Older releases and prereleases | Not supported |

## Scope

Please report vulnerabilities in:

- the Noctaya operator, controllers, gateway, and model-resolution or workload-building logic;
- the `LLMService` and `InferenceRuntime` APIs when their behavior exposes credentials, data, or
  cluster privileges;
- the Helm chart, Kustomize manifests, RBAC, official container images, or release workflow; and
- Noctaya's integration behavior when it makes an otherwise upstream issue exploitable through a
  Noctaya-managed deployment.

Vulnerabilities solely in Kubernetes, KEDA, vLLM or a vendor plugin, an accelerator driver or
device plugin, Volcano, or another third-party component should normally be reported to that
project. If you are unsure whether Noctaya contributes to the impact, report it privately here and
we will help route it.

The current alpha gateway intentionally has no built-in authentication. Exposing it directly
outside a trusted cluster boundary is unsupported; the absence of gateway authentication by itself
is a documented alpha limitation, not a previously unknown vulnerability.

## Reporting a vulnerability

**Do not open a public issue or discussion containing vulnerability details.**

Use [GitHub private vulnerability reporting](https://github.com/noctaya/noctaya/security/advisories/new)
when the repository's **Report a vulnerability** form is available. If it is unavailable, email
`jzlyy68@gmail.com` with `SECURITY: Noctaya` in the subject. Do not send credentials, private model
data, or unredacted production logs; we can arrange how to exchange sensitive evidence after the
initial contact.

Include as much of the following as possible:

- the affected component, release or commit, and deployment environment;
- the security impact, required privileges, and realistic attack prerequisites;
- reproducible steps and a minimal proof of concept, if safe to provide;
- relevant redacted logs, manifests, or request/response examples;
- known mitigations or a proposed fix; and
- your preferred credit and coordinated-disclosure timeline.

## Response process

- We aim to acknowledge a report within **3 business days** and provide an initial assessment
  within **7 business days**.
- We will keep the reporter informed at least every **7 business days** while an accepted report is
  active, unless another cadence is agreed.
- Remediation timing depends on severity, exploitability, and release risk. Alpha support is
  best-effort and does not provide a security SLA.
- When appropriate, we will prepare the fix privately, publish a GitHub Security Advisory, request
  a CVE, identify affected versions and mitigations, and credit the reporter unless anonymity is
  requested.
