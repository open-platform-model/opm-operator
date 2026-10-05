## Why

`Ready=True` on a ModuleInstance or ModulePackage means the operator applied the render. It says nothing about whether the workloads it applied have rolled out. A Deployment whose new pods crash-loop, a StatefulSet that never reaches its replica count, or a Job that never completes all leave the instance `Ready=True`, and nothing the operator writes tells a reader otherwise. The cli has judged readiness for `opm instance status` for a long time. Library `v1.0.0-beta.6` moved that evaluator into the Kubernetes tier as `opm/k8s/health` (library#199, 0012:D3): a pure package that judges objects the caller fetches, plus `ProgressDeadlineExceeded`, which separates a stalled Deployment rollout from one still in progress.

The owner decided how the operator uses it. The operator adds a separate `Healthy` condition and requeues until the instance has rolled out. `Ready` keeps meaning "applied" for now, and whether `Ready` should require `Healthy` is left for a later decision. The cli switches to the same package in its own change.

## What Changes

- **A new `Healthy` condition** on ModuleInstance and ModulePackage, with four reasons:
  - `RolledOut` (`True`): every inventory object was read after the apply it reflects, and `health.Evaluate` judged each one healthy.
  - `NotRolledOut` (`False`): at least one object is not healthy yet or is missing from the cluster.
  - `ProgressDeadlineExceeded` (`False`): at least one Deployment reports its rollout stalled (`health.ProgressDeadlineExceeded`).
  - `HealthUnknown` (`Unknown`): the inventory is empty, or an object could not be read.
  The message gives the ready count and the total, and names the first objects that are not ready.
- **When it is judged.** After every reconcile that leaves the object `Ready=True` with reason `ReconciliationSucceeded`: a successful apply (after prune), a `NoOp`, and a reconcile that skips its render because the inputs are unchanged. The skip, which today patches nothing, now patches the `Healthy` condition and nothing else, and only when the condition changes. A failed, refused, suspended or timed-out reconcile leaves `Healthy` as it was.
- **How it is judged.** Each object in the inventory is read uncached, after the apply, through the identity that applied it: the impersonated ServiceAccount client, or the manager's uncached API reader when no ServiceAccount applies. Reads run in parallel with a fixed bound, under one deadline. `health.Evaluate` judges each object, and `health.Aggregate` folds the results. A missing object counts as not ready. The operator never sets `Healthy=True` from a read taken before the apply.
- **Requeue until rolled out.** `NotRolledOut`, and `HealthUnknown` caused by a read failure, requeue after half the time since `status.lastAppliedAt`, with a floor of 5 seconds and a ceiling of 2 minutes. `ProgressDeadlineExceeded` stops the fast requeues and rechecks after the stalled interval of 30 minutes. `RolledOut` and an empty inventory do not requeue. A ModulePackage requeues after the shorter of this and `spec.interval`. A health requeue does not set `nextRetryAt`, because nothing failed.
- **Unchanged.** `Ready`, its reasons and its timing. `spec.dependsOn` still waits on `Ready`. A TransformerRegistration still activates on its provider's `Ready`. No apply, prune, digest or history entry changes.
- **Stale conditions.** A CLI-owned instance (`ManagedExternally`) and the operator's own refused instance (`SelfManagementRefused`) have `Healthy` removed, like the other conditions the operator no longer maintains on them.
- **API.** A `Healthy` printer column on both kinds, the regenerated CRDs, `dist/install.yaml`, and the operator module's generated CRD data.
- **Docs.** The `Healthy` rows on the operator conditions page, and the skip's new health read in `docs/RENDERING.md`.

Out of scope: making `Ready` depend on `Healthy` (an owner decision for later); the cli's switch to `opm/k8s/health` (its own change); events or metrics for health transitions; caching impersonated clients across reconciles.

## Classification

Additive: one new condition type, a printer column and four reasons on v1alpha1, with no field removed and `Ready` unchanged. MINOR after GA; before GA it ships as the next `-beta.N` under a `feat` title. Complexity (Principle VII): one evaluation function and one requeue function in `internal/reconcile`, called from three places in each reconciler. The bounded parallel reads cost about 40 lines and stop a large inventory from holding a reconcile worker for N serial round trips.

## Capabilities

### New Capabilities

- `instance-health`: the `Healthy` condition: when the operator judges it, through which identity it reads, how the library's verdict maps to reasons, and how it requeues.

### Modified Capabilities

- `status-conditions`: the `Healthy` condition type, its four reasons and its helper.
- `reconcile-loop-assembly`: "Status always patched": a skipped render patches only a changed `Healthy` condition.
- `modulepackage-reconcile-loop`: "Status always patched": the same for a ModulePackage.
- `render-input-key`: "A reconcile skips its render when its inputs are unchanged": the skip judges health, and a skipped ModuleInstance that has not rolled out requeues.
- `module-instance-ownership`: the `ManagedExternally` acknowledgement and the self-management refusal remove `Healthy`.
- `kubernetes-tier-adoption`: the operator judges readiness only through `opm/k8s/health` and keeps no evaluator of its own.

## Impact

- API: `api/v1alpha1/moduleinstance_types.go` and `modulepackage_types.go` (printer column). Regenerated: `config/crd/bases`, `dist/install.yaml`, `modules/opm_operator/zz_generated_crds.cue`. Because of the generated module data, the PR's squash commit also lands in the operator module's changelog (repo `AGENTS.md`, "This repository releases two units").
- Code: `internal/status/conditions.go`, `internal/reconcile/health.go` (new), `internal/reconcile/moduleinstance.go`, `internal/reconcile/modulepackage.go`.
- Tests: unit tests for the evaluation and requeue functions, envtest specs for both reconcilers. The render-skip specs that expect "patches nothing" seed a `Healthy=True` condition first. An e2e assertion that the podinfo instance reaches `Healthy=True`.
- Docs: `docs/site/diagnostics/operator-conditions.md`, `docs/RENDERING.md`.
- RBAC: none. The reads use the identity that applied the objects, which can already read them for server-side apply. The manager's reader is used only where the manager applied them itself.
- Dependencies: none. The operator already pins library `v1.0.0-beta.6` (opm-operator#248), which has `opm/k8s/health` with `ProgressDeadlineExceeded`.
- Release: held with the inventory-adoption and ownership-adoption changes, so they reach users in one operator release.
- Enhancement: `enhancement.yaml` declares 0012 and claims no decision. The health package belongs to 0012:D3, and its claim waits until the cli also judges health only through it (0012:D1).
