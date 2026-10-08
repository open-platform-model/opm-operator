## Why

The library holds one ownership rule for Kubernetes objects (`opm/k8s/ownership`, library v1.0.0-beta.7), and the cli judges every apply, prune and delete with it (cli#347, cli#349). The operator still decides with code of its own: its prune compares labels in `internal/apply/prune.go`, it sends no delete precondition, it knows nothing of the adopt annotation, and its apply has no ownership guard at all. So the two managers judge the same object differently, and the operator can prune an object a user is handing to another instance (enhancement 0012:D4, 0012:D8).

## What Changes

This change is an API addition. It adds three optional status fields: `status.instanceUUID` and `status.previousInstanceUUID` on ModulePackage, and `status.previousInstanceUUID` on ModuleInstance. The PR therefore regenerates the CRDs, the resource reference and the operator module data (`modules/opm_operator/zz_generated_*`), and opens a release PR of the operator module, as the repo rules state for every CRD change.

The operator's adoption is cut into two changes, as the cli's was. This change is the delete half. The apply half is the change `guard-every-apply-by-ownership`; `design.md` records its decisions so that both halves follow one design, and it gets its own change folder after this one.

Why this half is safe to ship alone: it narrows what the operator deletes, makes each delete exact, and deletes more only in the two cases the owner decided (stale objects after an identity change; a same-named kind in another group). Until the apply half lands, the apply keeps taking over what it takes over today.

This change:

- **BREAKING** Every delete of a prune of stale objects and of a deletion cleanup, for a ModuleInstance or a ModulePackage, is judged by `ownership.CanDelete`. The label checks in `internal/apply/prune.go` go.
- **BREAKING** An object whose `opmodel.dev/adopt` annotation names another instance is never deleted. It is left in the cluster and leaves `status.inventory` (0012:D8:R8).
- **BREAKING** A Namespace or a CustomResourceDefinition is matched by group and kind, as the library does, not by kind alone.
- Every delete carries the UID precondition of the object that was read: in a prune, in a deletion cleanup and in the forced recreate of `spec.rollout.forceConflicts`. The forced recreate asks no ownership question (owner decision of 2026-10-08); it deletes exactly the object that was read. A delete the API server refuses on the precondition is a failed delete and is retried.
- **BREAKING** After an identity change (a changed `spec.module.path`), the status holds the earlier and the new identity until a reconcile has applied and pruned with success (owner decision of 2026-10-08). The stale prune and the deletion cleanup accept an object that carries either. So the stale objects of the earlier identity are deleted, where today they are abandoned, and an instance deleted in that window leaves nothing behind.
- **BREAKING** A second identity change before the first is settled is refused: `Ready=False`, `Stalled`, reason `IdentityChangeUnsettled`.
- A ModulePackage records its instance identity (0012:D8:R4, owner decision of 2026-10-03), so its prune and deletion compare identities as a ModuleInstance's do. Today they compare none.
- A prune or a deletion cleanup that leaves objects behind emits one event with reason `LeftBehind`, with the library's message for each object: `Normal` when only Namespaces or CustomResourceDefinitions were left, `Warning` otherwise. Today a skip is a log line only.
- The resource manager's client refuses a delete of a collection, and a test lists the allowed delete call sites, so no delete can go out without a precondition unseen.

Not in this change: the apply guard, the hand-over on apply and the drop of an adopted object from the inventory on apply (the apply half); the deletion protocol (order, finalizer holds, propagation: the change that adopts `opm/k8s/lifecycle`); RBAC; the cli.

SemVer: MAJOR after GA. During beta it ships as the next `-beta.N` under a `feat!` title, in the same operator release as the apply half.

Built on, and not changed by this change: opm-operator#260 (narrowed impersonation), #261 (periodic reconcile and restore), #263 (stuck deletes), #262 (held claims), #265 (drift as the identity that applies), #267 and #268 (`spec.dataPolicy` on prune, deletion and forced recreate), #269 (library beta.7).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `prune-stale-resources`: the live-state ownership guard becomes the library's delete verdict; kinds OPM never deletes are matched by group and kind; deletes carry the UID precondition; the prune judges with the recorded identities; the prune reports what it left behind.
- `finalizer-and-deletion`: the deletion cleanup deletes what the delete verdict lets it delete, with the recorded identities; a skipped object does not hold the finalizer, a delete refused on its precondition does.
- `reconcile-loop-assembly`: the identities are stored before the first write of an apply and the earlier one is cleared on full success; a second unsettled identity change is refused.
- `ssa-apply`: the delete of a forced recreate carries the UID precondition; the resource manager refuses a delete of a collection.
- `modulepackage-reconcile-loop`: a ModulePackage records its instance identity and prunes and deletes with it.
- `status-conditions`: the reason `IdentityChangeUnsettled`.
- `events-emission`: the `LeftBehind` event and the `IdentityChangeUnsettled` event.
- `kubernetes-tier-adoption`: the operator decides the deletes of a prune and a cleanup only through the library's ownership package, and the list of delete call sites is closed.

## Impact

- API: `ModulePackageStatus.InstanceUUID`, `ModulePackageStatus.PreviousInstanceUUID`, `ModuleInstanceStatus.PreviousInstanceUUID`; all additive and optional. No spec field changes.
- Controllers: ModuleInstance and ModulePackage. Platform and TransformerRegistration apply and delete no cluster object and are not touched.
- Code: `internal/apply` (`prune.go`, `claims.go`), `internal/reconcile` (`moduleinstance.go`, `modulepackage.go`), `internal/status` (two reasons), `api/v1alpha1`, `docs/site/`.
- Enhancement: implements part of 0012 (`enhancement.yaml`). No decision is claimed until both frontends have adopted both packages.
- RBAC: none. The verdict reads each object with the identity that deletes it, which already needs `get` for the prune's read today; the forced recreate adds no read.
- cli: none needed. The cli does not know `status.previousInstanceUUID`; a follow-up can teach it (see `design.md`, the handover section).
