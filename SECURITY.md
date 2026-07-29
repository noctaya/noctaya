# Security Policy

Noctaya is alpha software that reconciles Kubernetes workloads and proxies inference traffic. Please report suspected vulnerabilities privately.

## Supported versions

Only the newest published release is supported, including a prerelease when no newer stable release exists.

## Report a vulnerability

**Do not disclose vulnerability details in a public issue or discussion.**

Use [GitHub private vulnerability reporting](https://github.com/noctaya/noctaya/security/advisories/new).

Include:

- the affected component, release or commit, and deployment environment;
- the impact, required privileges, and realistic attack prerequisites;
- reproducible steps or a minimal proof of concept; and
- any known mitigation and your preferred credit.

## Scope

Reports may cover the operator, gateway, APIs, model resolution, generated workloads, Helm and Kustomize packaging, RBAC, official images, and release automation. Report vulnerabilities in Kubernetes, KEDA, inference runtimes, vendor plugins, drivers, device plugins, schedulers, or other third-party components to their upstream projects unless Noctaya's integration creates the
security impact. If ownership is unclear, report privately here and we will help route it.

The alpha gateway has no built-in authentication. Direct exposure outside a trusted boundary is unsupported.

## Response

- We aim to acknowledge reports within **3 business days**, provide an initial assessment within
  **7 business days**, and send updates at least every **7 business days** while a report is active.
- Remediation depends on severity, exploitability, and release risk. Alpha support is best-effort and does not provide a security SLA.
