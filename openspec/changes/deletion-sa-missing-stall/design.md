## Context

Observed behavior (kind smoke test, 2026-04-19, hello fixture): operator runs `kubectl delete -f modulerelease.yaml`, which bundles the impersonation SA (`hello-applier`), its `Role`, `RoleBinding`, and the `ModuleRelease` itself. Kubectl deletes all objects concurrently; the SA wins the race against the finalizer. Inside `handleDeletion`:

```
mr.Spec.ServiceAccountName != "" && params.RestConfig != nil
  → apply.NewImpersonatedClient returns "serviceAccount default/hello-applier not found"
  → INFO "ServiceAccount unavailable for deletion cleanup, using controller client"
  → deleteClient = params.Client (controller's narrow identity)
  → apply.Prune → forbidden on configmaps → error returned to controller-runtime
  → finalizer retained → reconcile re-queued → loops forever
```

The same pattern exists verbatim in `internal/reconcile/release.go` around line 613.

The silent fallback is actively harmful in this case. The controller's narrow RBAC means the fallback always fails; the operator sees confusing logs ("User \"system:serviceaccount:opm-operator-system:opm-operator-controller-manager\" cannot get resource \"configmaps\"") that misattribute the problem to the controller's RBAC rather than the missing target SA.

Related code:
- `internal/apply/impersonate.go:NewImpersonatedClient` returns an error whose unwrap chain includes `apierrors.IsNotFound` for the SA lookup.
- `internal/status/conditions.go` defines `ImpersonationFailedReason` used on the apply path and the existing deletion path.

## Goals / Non-Goals

**Goals:**
- Turn the silent-fallback + forever-retry into a visible, actionable stall.
- Give operators a deterministic one-way exit for the stuck state via a metadata annotation.
- Apply the change symmetrically to both `ModuleRelease` and `Release` deletion paths (same bug in both).
- Preserve existing behavior for all non-NotFound impersonation errors (transient API errors, RBAC-denied on `impersonate`, etc.) — those keep the generic `ImpersonationFailed` reason and requeue.

**Non-Goals:**
- No owner-reference stamping on applied resources (architectural lever, separate change).
- No ordered-prune based on kind metadata (does not help this scenario; SA was deleted externally).
- No admission webhook rejecting self-referencing SA renders.
- No fixture change. The `hello` fixture's bundled-SA layout is now a regression probe for the new stall-and-orphan-exit behavior.
- No change to the apply path. Apply-time SA-missing already stalls with `ImpersonationFailed`; that remains.
- No automatic orphan. The escape hatch is explicitly operator-driven — annotation required.

## Decisions

### D1: New condition reason `DeletionSAMissing`, distinct from `ImpersonationFailed`

The apply-path reason is `ImpersonationFailed` (generic). The deletion-cleanup SA-missing case has a specific recovery flow (restore SA, set prune=false, or orphan-annotate). Giving it its own reason makes status filtering trivial and makes the message template focused.

**Alternatives considered:**
- Reuse `ImpersonationFailed` with a distinct message. Rejected: clients filtering by reason lose the ability to distinguish "transient, will resolve" from "operator action required, will not self-resolve".
- Emit as `PruneFailed`. Rejected: the root cause is the SA, not the prune logic; `PruneFailed` would attribute the failure incorrectly and collide with existing prune-error semantics.

### D2: Detect "SA does not exist" via `apierrors.IsNotFound` on the wrapped error

`NewImpersonatedClient` today returns a wrapped error: `fmt.Errorf("serviceAccount %s/%s not found: %w", namespace, saName, err)`. The inner error is a K8s `NotFound`. Callers detect it with `errors.Is(err, ...)` or `apierrors.IsNotFound(err)` via `errors.As`/unwrap.

Because the wrapping preserves the chain, the detection is reliable. Adding a sentinel error type is unnecessary YAGNI.

**Implementation sketch:**

```go
func isSANotFound(err error) bool {
    return apierrors.IsNotFound(err)
}
```

### D3: Annotation name `opm.dev/force-delete-orphan`

Boolean semantics: presence with value `"true"` enables orphan-exit. Any other value (including empty) is treated as absent. Non-spec metadata = correct surface for an emergency operator lever that does not deserve a CRD field.

**Alternatives considered:**
- CRD field `spec.orphanOnDeletion`. Rejected: implies a normal-path behavior; this is a recovery lever. Adding a CRD field pushes operators toward making it a default rather than a break-glass.
- Label instead of annotation. Rejected: labels are for selection/indexing. This is a one-shot per-instance decision.
- Annotation value must equal the release's current generation, to prevent stale annotations. Rejected: YAGNI; if the operator set it and forgot, they can remove it. Generation-pinning adds complexity for a vanishingly rare case.

### D4: Orphan-exit only applies when SA is the blocker

The annotation's effect is scoped:

- SA missing (NotFound from impersonation client build): annotation → skip prune, remove finalizer, emit Warning event.
- Transient impersonation error (connection refused, 5xx, etc.): annotation ignored; existing stall + requeue behavior.
- Prune partial failure with the impersonated client working fine (e.g., some resources forbidden because the SA's Role was narrowed): annotation ignored; existing `PruneFailed` behavior.

Rationale: the annotation is a "my SA is gone, get me out" lever, not a general-purpose "skip deletion cleanup". Narrow scope keeps the lever from being misused to bypass legitimate prune errors.

### D5: Requeue interval on `DeletionSAMissing` stall

Longer than the transient backoff. Operator-attention time. Align with existing `StalledRecheckInterval` if one exists in the package; otherwise use a small constant like 2 minutes. A stuck release does not need tight-loop requeues.

Actual choice defer to implementation — pick whatever matches existing stalled-requeue conventions in the reconcile package.

### D6: Event emissions

- On entering `DeletionSAMissing` stall: `Warning` event, reason `DeletionSAMissing`, message naming the SA and suggesting recovery (first occurrence per Ready transition only; avoid event spam on requeue).
- On orphan-exit via annotation: `Warning` event, reason `OrphanedOnDeletion`, message naming the number of inventory entries that were NOT pruned. Higher severity than the routine delete event because it represents a true leak operators should audit.

### D7: Log output

Replace the current `INFO "ServiceAccount unavailable for deletion cleanup, using controller client"` with:

- If SA NotFound and no orphan annotation: `ERROR "Impersonation ServiceAccount missing during deletion; release stalled pending operator action"` with structured keys `serviceAccount`, `namespace`, `annotation` (set to the annotation name so operators can grep the runbook).
- If SA NotFound and orphan annotation present: `WARN "Orphaning inventory and removing finalizer at operator request"` with `inventoryCount`.
- Other impersonation errors: unchanged.

### D8: Message template for the stall condition

Fixed template, substituting the SA name:

```
ServiceAccount "<ns>/<sa>" not found; cannot prune owned resources during deletion.
Recovery options:
  1. Restore the ServiceAccount and its RBAC.
  2. Set spec.prune=false on the release and delete again to orphan resources without prune.
  3. Add annotation "opm.dev/force-delete-orphan=true" to the release to remove the finalizer
     and leave resources behind (operator is responsible for cleanup).
```

Verbose by status-message standards. Justified: the whole point is replacing "read the controller logs" with "read the status message".

## Risks / Trade-offs

- **Risk:** Operators use the orphan annotation as a default workaround instead of fixing the underlying cause (e.g., the bundled-SA anti-pattern). → **Mitigation:** name and event phrasing emphasize leak/audit; docs describe it as a break-glass.
- **Risk:** Integration tests become flaky due to timing of SA deletion vs finalizer run. → **Mitigation:** test explicitly deletes the SA then patches the release for deletion, doesn't race them.
- **Trade-off:** `Release` and `ModuleRelease` deletion paths get parallel-but-separate edits. Could refactor into a shared helper. Accept duplication for this change (scope discipline per Principle VIII); refactor later if a third caller appears.
- **Trade-off:** Orphan annotation survives deletion in the etcd tombstone window but not beyond. An operator who removes the finalizer with the annotation and then recreates the release with the same name sees no trace of the orphan. Acceptable: the inventory-orphan event is the durable record.

## Migration Plan

Behavior change affects only the deletion path, only the specific SA-NotFound case. Existing deletions that succeed today continue to succeed. Deletions that loop forever today will now stall visibly. Operators of currently-stuck releases gain a documented path out (the annotation); deployments without stuck releases see no difference.

No data migration. No CRD schema change.

Rollback: revert the code. Stuck-release annotations on previously-orphaned releases become no-ops; no cleanup required.

## Open Questions

1. Should the orphan-exit also clear `status.inventory` (since the controller is dropping the finalizer without pruning)? Lean yes — otherwise the last-observed status shows inventory the controller no longer intends to manage. Confirm during implementation; not load-bearing for the core fix.
2. Does `BundleRelease` deletion also impersonate? Grep shows no deletion-time impersonation there (BundleRelease orchestrates children only). Confirm during implementation; if it does, apply the same change. If not, leave out of scope.
3. The Flux `kustomize-controller` has the same bug class. Confirm their current behavior in a follow-up; if they've solved it differently (e.g., SAR pre-check before prune), consider aligning. Not load-bearing for the immediate fix.
