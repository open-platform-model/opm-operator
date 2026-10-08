## Why

A ModuleInstance that reached `Ready` with a rolled-out inventory is not reconciled again until its spec, the Platform or the operator changes: the reconcile returns no requeue (`internal/reconcile/health.go`, `healthRequeue`) and nothing watches the applied objects. `Healthy` and `Drifted` then show the state of the last reconcile for hours or days, and an object deleted by hand stays deleted (opm-operator issues 34 and 35; ADR-016 records periodic level-triggering for ModuleInstance as planned, not built). ModulePackage does not have this gap, because it requeues on `spec.interval`.

A requeue alone does not bring a deleted object back. An unchanged instance ends as a `NoOp`, and a `NoOp` skips the apply. The dry-run that drift detection already sends sees that the object is missing, but nothing reads that answer today.

## What Changes

- A ModuleInstance whose reconcile ends well (applied, `NoOp`, or a skipped render) and whose health asks for no requeue is requeued after a fixed interval. The interval is a new operator flag, `--instance-reconcile-interval`, default `10m`, with up to 10 percent added at random so instances do not line up. `0` disables it and gives the behaviour of today. A negative value stops the operator at start.
- No CRD change: there is no `spec.interval` on ModuleInstance.
- Suspended instances, CLI-owned instances and the operator's own instance are not requeued, as today. Failure, stalled and health requeues keep their own intervals.
- A reconcile that renders and finds all digests unchanged applies the rendered objects that do not exist on the cluster, and only those. It does not apply objects that exist, so drift on a live object is still reported and never corrected (ADR-012). A Job that sets `ttlSecondsAfterFinished` is not restored: its absence after it finished is the expected state, and to create it again runs it again.
- The periodic reconcile of an unchanged instance costs one health judgement (uncached reads of the inventory) per interval and one render plus one dry-run per `--drift-render-interval` (default 30m). It writes nothing while nothing changed, except the `lastAppliedInputs.renderedAt` move a rendering `NoOp` already makes.
- A new ADR records the choice of a fixed operator-wide interval over a `spec.interval` field or a watch on child objects, and replaces the "planned mechanism" sentence of ADR-016.

SemVer: PATCH after GA (a bug fix: no API, flag removal or status contract change; one new optional flag with a safe default). Beta ships it as the next `-beta.N`. Not breaking. One behaviour change an operator can notice: an object of an instance that was deleted by hand comes back.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `reconcile-loop-assembly`: a new requirement, a healthy ModuleInstance is reconciled periodically on the operator's interval; suspended and CLI-owned instances are not.
- `instance-health`: the requeue requirement now says a rolled-out ModuleInstance requeues on the instance reconcile interval instead of not at all.
- `drift-detection`: a new requirement, an object the render produces and the cluster lacks is created again on a `NoOp`, and a Job with a TTL is left out.

## Impact

- Code: `internal/reconcile/moduleinstance.go` (requeue on the three healthy returns, the restore step), `internal/apply/drift.go` (report missing objects beside drifted ones), `internal/controller/moduleinstance_controller.go` and `cmd/main.go` (the flag and its wiring).
- API types and CRDs: none. RBAC: none. ModulePackage and Platform controllers: none.
- Docs: `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`, `adr/` (a new ADR, a status note on ADR-016, a pointer in ADR-012).
- The operator module (`modules/opm_operator`) does not mirror operator flags, so it does not change; the default applies to it.
- Load: with N healthy instances the operator makes N health judgements per 10 minutes and N renders per 30 minutes. With the default single render slot the renders run one at a time; see design.md for the numbers and the limit.
