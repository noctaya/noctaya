# Release validation

Every release candidate must pass the hardware-independent gates on disposable Kind and one representative physical-accelerator lifecycle. Kind proves Kubernetes behavior; it does not prove vendor runtime or device support.

## Automated gates

Run:

```bash
make test
make lint
make test-docs
make test-e2e
make test-release
```

`make test-release` refuses shared contexts. It creates a dedicated Kind cluster with two schedulable workers, installs KEDA, installs the last compatible release, and then:

1. creates an `InferenceRuntime` and `LLMService` and records their owned-resource UIDs;
2. server-side dry-runs and applies candidate CRDs;
3. upgrades to the candidate chart and verifies status, ownership, inference, two-replica operator placement, and disruption protection;
4. rolls back the controller while retaining the upgraded CRDs, then proves the previous controller can still reconcile;
5. upgrades again and replaces the active operator, gateway, and backend;
6. restarts a Kind worker node and verifies reconciliation and inference after recovery; and
7. deletes the cluster after writing logs, versions, timings, Helm history, and final resources to `bin/release-validation/`.

The default baseline is `v0.4.0-alpha.1`, the first release using the current Noctaya API. Set `RELEASE_PREVIOUS_VERSION` when the supported upgrade baseline changes. Releases before `v0.4.0-alpha.1` use the former API identity and require migration rather than in-place upgrade.

Operator handoff is measured from leader deletion until the replacement holds the Lease and reconciliation succeeds. The gate allows 30 seconds; the actual duration is retained in `test.log`.

Helm installs but does not upgrade or roll back files under `crds/`. The gate therefore server-side dry-runs the candidate schemas and deliberately transfers their field ownership from Helm to `noctaya-release-validator` with `--force-conflicts` before the chart upgrade. A failed dry run, an old controller that cannot reconcile after rollback, changed workload ownership, or replaced workload UIDs fails the gate and requires an explicit migration plan.

The evidence directory contains the exact shell trace in `commands.log`, Ginkgo results and timings in `test.log`, environment and Helm history in `environment.txt`, and final API objects in `resources.yaml`.

## Candidate checklist

Before tagging:

- update the chart and application versions, changelog, and supported baseline;
- pass the automated gates above;
- inspect `bin/release-validation/` and retain the CI artifact;
- run the External Push lifecycle on at least one representative physical accelerator with the exact candidate operator, gateway, and runtime images;
- record Kubernetes, KEDA, driver, device-plugin, image and chart versions, commands, timings, outcomes, and limitations; and
- add new evidence without rewriting historical hardware reports.

Follow the [hardware validation requirements](requirements.md) for the physical run. A release is not qualified if either the disposable-Kind gate or the representative accelerator lifecycle is missing.
