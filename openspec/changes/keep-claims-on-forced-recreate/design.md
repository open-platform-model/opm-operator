## Context

`apply.Apply` passes `spec.rollout.forceConflicts` to Flux as `ApplyOptions.Force`. In `fluxcd/pkg/ssa` v0.77.0, `ApplyAll` dry-runs every object of a stage in parallel. When a dry-run fails with an error that `IsImmutableError` accepts (Invalid, Conflict, or a message that says "immutable") and `Force` is set, it deletes the live object, waits for it to go, and dry-runs again (`manager_apply.go:250-275`). Only after every dry-run of the stage passed does it apply. Flux offers `Force` for all objects and `ForceSelector` by label or annotation; it has no exclusion by kind.

For a PersistentVolumeClaim the API server refuses a change of `storageClassName`, `accessModes`, `volumeName`, `volumeMode`, and a lower storage request, with an Invalid error on the field `spec`. The forced recreate then deletes the claim. The volume and its data follow under the reclaim policy.

#267 put the claim protection in `apply.Prune` and stated the apply path as an exception.

## Goals / Non-Goals

**Goals:**

- Under `Keep` or no `dataPolicy`, no code path of the apply deletes a core PersistentVolumeClaim.
- The user sees which claim, which field, and what to do.
- A refused claim leaves the cluster as it was: no half-applied render.
- `Delete` keeps today's behaviour. Other kinds keep today's behaviour.

**Non-Goals:**

- Prune and deletion (#267). Other storage kinds. Claims from `volumeClaimTemplates` (never rendered). The cli. An apply without `forceConflicts`. A new API field.

## Decisions

### 1. `Apply` takes options, and the zero value protects

```go
type ApplyOptions struct {
    // Force recreates an object the API server refuses to update.
    Force bool
    // DeleteData lets Force delete and recreate a PersistentVolumeClaim.
    DeleteData bool
}

func Apply(ctx context.Context, rm *fluxssa.ResourceManager,
    resources []*unstructured.Unstructured, opts ApplyOptions) (*ApplyResult, error)
```

The reconcilers set `DeleteData` from `spec.dataPolicy.DeletesClaims()`, the same call the prune uses. `DeleteData` MUST NOT depend on `spec.prune`: the forced recreate is not a prune.

### 2. A claim check before anything is applied

When `Force` is true and `DeleteData` is false, `Apply` MUST check every core PersistentVolumeClaim of the set before the staged apply:

```go
for _, claim := range claims(resources) {
    live, err := get(claim)          // NotFound: nothing to protect, next
    err = dryRunApply(claim)         // same options as Flux: DryRunAll, ForceOwnership, FieldManager
    if err != nil && ssaerrors.IsImmutableError(err) {
        return nil, &ClaimConflictError{Namespace, Name, Fields: causes(err), Cause: err}
    }
}
```

The check uses the predicate Flux uses, so it refuses exactly the cases in which Flux would delete. A read error other than NotFound fails the apply. A dry-run error that is not an immutable error is left to the staged apply, which reports it as today.

The refused field comes from the `causes` of the API status (`spec` for a claim); the API server's message is carried in full.

### 3. Nothing of the render is applied when a claim is refused

The check runs before the first stage, so a refused claim stops the apply with the cluster unchanged. This is the safer choice, for three reasons:

- Flux deletes other immutable objects of the same stage in the dry-run pass, before it knows that the stage fails. Without the early check, a ConfigMap could be deleted and never created again while the claim keeps failing the stage.
- A workload of the new render applied against a claim of the old shape runs a version of the module that the module author never rendered.
- It equals what an apply without `forceConflicts` does for the stage of the claim, so the user meets one behaviour.

No prune runs either: the reconcilers prune only after a successful apply.

### 4. The resource manager cannot delete a claim on its own

The check and the staged apply are two requests, and a live claim can change between them. To close that window, `NewResourceManager` MUST wrap the client it hands to Flux: `Delete` and `DeleteAllOf` of a core PersistentVolumeClaim return a `ClaimConflictError` unless the context carries the permission that `Apply` sets when `DeleteData` is true. The permission is a private context key of the package. The wrapper protects at the zero value: a caller that forgets the option cannot delete a claim through the apply. `apply.Prune` does not use the resource manager's client and is not affected.

In that rare race the stage fails and the claim survives; other objects of the stage that Flux already deleted come back on the next reconcile once the conflict is resolved. Data is never lost.

### 5. Report: reason `ClaimConflict`, not stalled

Both reconcilers detect the typed error with `errors.As` and report:

- `Ready=False`, reason `ClaimConflict`, message: the claim as `<namespace>/<name>`, the refused field, the API server's message, and the ways out.
- One `Warning` event, reason `ClaimConflict`, action `Apply`, the same text.
- Outcome `FailedTransient`, retried on the bounded backoff. Not `Stalled`: one way out is to delete or change the claim by hand, which does not touch the instance and which no watch reports. The backoff (at most five minutes) finds it. `DependentsRemain` is classified the same way for the same reason.

The message:

```text
PersistentVolumeClaim media/config: the API server refused the update of spec (<API message>). The claim and its data are kept and nothing was applied. Revert the change, or move the data and delete the claim yourself, or set spec.dataPolicy to Delete to let the operator delete and recreate the claim.
```

The apply failure counter counts it like any failed apply.

### 6. `Delete`

With `spec.dataPolicy: Delete` and `forceConflicts`, the check is skipped, the context carries the permission, and Flux recreates the claim as today. `spec.prune` is not read.

### 7. Reconcile phase impact

- Source, Render: none.
- Apply: the check of decision 2 before the staged apply, only when `forceConflicts` is set and the policy keeps claims. One GET per rendered claim, and one dry-run per live claim.
- Prune: none; it does not run after a refused apply.
- Status: the new reason on `Ready`.

### 8. Security

No new permission: the check reads and dry-runs claims the apply identity already reads and patches. The event text holds object names and an API validation message, no secret. The message goes to the status of an object the tenant owns.

### 9. Record

ADR-020 is amended: the exception paragraph and its negative consequence are replaced by the rule.

## Research & Decisions

### How to exempt a kind from Flux's force

**Context**: Flux has no exclusion by kind for `Force`.
**Explored**: (a) `Force=false` plus `ForceSelector` on a label stamped on every non-claim object: writes a label to user objects. (b) Two staged applies, claims without force: breaks the stage order (a claim's Namespace is in stage one; claims are in the last stage). (c) A check before the apply. (d) A client that refuses claim deletes.
**Decision**: (c) and (d) together.
**Rationale**: (c) gives the all-or-nothing behaviour and the field name; (d) makes the guarantee hold without a time window. Each is small. (d) alone leaves other objects deleted and not recreated.

### Stalled or transient

**Context**: The user must act, which reads like a stall.
**Explored**: `MarkStalled` with the 30-minute recheck against the transient backoff.
**Decision**: transient.
**Rationale**: decision 5. A stall would leave a user who deleted the claim by hand waiting up to 30 minutes.

## Risks / Trade-offs

- [An instance that relied on the recreate of a claim now stops] → the message names `spec.dataPolicy: Delete`; the PR title carries `!`; the docs page says it.
- [`IsImmutableError` accepts every Invalid error, so an invalid claim spec on a live claim is also reported as `ClaimConflict`] → correct for safety: Flux would have deleted the claim in that case too. The API message is in the text.
- [A context value steers the delete guard] → private key, set in one place, read in one place, covered by a test.
- [One more GET and dry-run per claim] → only under `forceConflicts` with a keeping policy.

## Migration Plan

None for stored objects. A user who wants the old behaviour sets `spec.dataPolicy: Delete`. Rollback is a revert of the PR.
