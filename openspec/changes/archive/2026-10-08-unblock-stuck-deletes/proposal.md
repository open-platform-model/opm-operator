## Why

Two deletes of a `ModuleInstance` need a manual fix or a long wait:

- An instance that was operator-owned carries the `opmodel.dev/cleanup` finalizer. When its `spec.owner` is then set to `cli` and it is deleted, the owner-skip gate returns without touching the finalizer, so the object stays in Terminating until someone strips the finalizer by hand. `spec.owner` is mutable, so this is a legal sequence.
- An instance whose delete is stalled with `DeletionSAMissing` tells the user to restore the ServiceAccount or to set the `opm.dev/force-delete-orphan` annotation. Neither wakes the instance: the primary watch passes generation changes only and nothing watches ServiceAccounts. The fix takes effect at the next stalled recheck, up to 30 minutes later.

The periodic reconcile of a healthy instance (ADR-019) covers neither. It requeues only a reconcile that ended well: a CLI-owned instance returns no requeue at all, and a stalled delete requeues on the 30 minute stalled recheck.

## What Changes

- The owner-skip gate releases the `opmodel.dev/cleanup` finalizer from a CLI-owned instance that is being deleted. It deletes and prunes nothing and writes no status. A live CLI-owned instance is left as it is, finalizer included.
- The `ModuleInstance` controller reconciles an instance that is being deleted when its `opm.dev/force-delete-orphan` annotation becomes `"true"`.
- The `ModuleInstance` controller watches ServiceAccounts (metadata only) and reconciles the instances that impersonate a ServiceAccount when it is created. This covers the stalled delete and the same stall on the apply path.
- The manager ClusterRole gains `list` and `watch` on `serviceaccounts`, which the watch needs. It grants `impersonate` on nothing new.

No CRD change. `ModulePackage` is not changed: its delete has the same 30 minute wait and is left for a change of its own.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `module-instance-ownership`: a deleting CLI-owned instance that carries the finalizer has it released without pruning; the own-instance requirement names that release.
- `finalizer-and-deletion`: the two recovery signals of a `DeletionSAMissing` stall trigger a reconcile promptly on a `ModuleInstance`.
- `serviceaccount-impersonation`: the creation of an impersonated ServiceAccount triggers a reconcile of the instances that name it; the manager role may list and watch ServiceAccounts.

## Impact

- Code: `internal/reconcile/moduleinstance.go` (owner-skip gate), `internal/controller/moduleinstance_controller.go` (predicate, ServiceAccount watch, RBAC marker).
- Generated: `config/rbac/role.yaml`, `dist/install.yaml`, `modules/opm_operator/zz_generated_rbac.cue`.
- Controllers: `ModuleInstance` only. API types: none.
- Deployment: an operator of this release needs the new ClusterRole. With the old role the ServiceAccount informer cannot sync, the `ModuleInstance` controller fails to start after the cache sync timeout (2 minutes), the manager returns that error and the operator process exits. It then restarts and fails the same way, so no controller of the operator works until the role is updated. Every shipped form of the role (kustomize, install manifest, operator module) carries the rule.
- SemVer: PATCH (a bug fix with no API change); the beta line ships it as the next `-beta.N`.
- Docs: the deletion pages under `docs/site/operating/` and `docs/RENDERING.md` or the ADR index where they name the wait.
