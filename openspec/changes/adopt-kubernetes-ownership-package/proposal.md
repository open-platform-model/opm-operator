## Why

The library holds one ownership rule for Kubernetes objects (`opm/k8s/ownership`, library v1.0.0-beta.7), and the cli judges every apply, prune and delete with it (cli#347, cli#349). The operator still decides with code of its own: its prune compares labels in `internal/apply/prune.go`, it sends no delete precondition, it knows nothing of the adopt annotation, and its apply has no ownership guard at all. So the two managers judge the same object differently, and the operator can prune an object a user is handing to another instance (enhancement 0012:D4, 0012:D8).

## What Changes

The operator's adoption is cut into two changes, as the cli's was. This change is the delete half. The apply half is the change `guard-every-apply-by-ownership`; `design.md` records its decisions so that both halves follow one design, and it gets its own change folder after this proposal is accepted.

Why this half is safe to ship alone: it only narrows what the operator deletes and makes each delete exact. Until the apply half lands, the apply keeps taking over what it takes over today.

This change:

- **BREAKING** Every delete the operator makes on behalf of a ModuleInstance or a ModulePackage is judged by `ownership.CanDelete`: the prune of stale objects, the deletion cleanup, and the delete inside a forced recreate (`spec.rollout.forceConflicts`). The label checks in `internal/apply/prune.go` go.
- **BREAKING** An object whose `opmodel.dev/adopt` annotation names another instance is never deleted. It is left in the cluster and leaves `status.inventory` (0012:D8:R8).
- **BREAKING** A Namespace or a CustomResourceDefinition is matched by group and kind, as the library does, not by kind alone. A kind named `Namespace` in another API group is no longer protected.
- Every delete after a verdict carries the UID precondition of the object that was judged. A delete the API server refuses on that precondition is a failed delete and is retried.
- **BREAKING** Prune judges with the recorded identity, `status.instanceUUID` as it was when the reconcile started, not with the identity of the new render. After a module path change the stale objects of the earlier identity are deleted; today they are skipped and abandoned.
- `status.instanceUUID` of a ModuleInstance is written with a successful apply only, so a failed apply after an identity change does not move the recorded identity.
- A ModulePackage gains the additive field `status.instanceUUID` (0012:D8:R4, owner decision of 2026-10-03), so its prune and deletion compare identities as a ModuleInstance's do. Today they compare none.
- **BREAKING** A forced recreate deletes an object only when the delete verdict lets it. When it does not, the apply is refused before its first write.
- A prune or a deletion cleanup that leaves objects behind emits one `Warning` event with reason `LeftBehind`, with the library's message for each object. Today a skip is a log line only.
- A test lists the allowed delete call sites, so a new delete cannot bypass the verdict unseen.

Not in this change: the apply guard, the hand-over on apply and the drop of an adopted object from the inventory on apply (the apply half); the deletion protocol (order, finalizer holds, propagation: the change that adopts `opm/k8s/lifecycle`); RBAC; the cli.

SemVer: MAJOR after GA. During beta it ships as the next `-beta.N` under a `feat!` title, in the same operator release as the apply half.

Built on, and not changed by this change: opm-operator#260 (narrowed impersonation), #261 (periodic reconcile and restore), #263 (stuck deletes), #262 (held claims), #265 (drift as the identity that applies), #267 and #268 (`spec.dataPolicy` on prune, deletion and forced recreate), #269 (library beta.7).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `prune-stale-resources`: the live-state ownership guard becomes the library's delete verdict; kinds OPM never deletes are matched by group and kind; deletes carry the UID precondition; prune judges with the recorded identity; the recorded identity is written with a successful apply; the prune reports what it left behind.
- `finalizer-and-deletion`: the deletion cleanup judges every inventory entry with the delete verdict, with the recorded identity; a skipped object does not hold the finalizer, a delete refused on its precondition does.
- `ssa-apply`: the delete of a forced recreate is judged by the delete verdict and carries the UID precondition.
- `modulepackage-reconcile-loop`: a ModulePackage records its instance identity in `status.instanceUUID` and prunes and deletes with it.
- `events-emission`: the `LeftBehind` event.
- `kubernetes-tier-adoption`: the operator decides deletes only through the library's ownership package and keeps no copy of the rule.

## Impact

- API: `ModulePackageStatus.InstanceUUID` (`status.instanceUUID`), additive and optional. No spec field changes. The CRD, the generated operator module data (`modules/opm_operator/zz_generated_*`) and the resource reference change with it, so the PR also opens a release PR of the operator module, as the repo rules state for every CRD change.
- Controllers: ModuleInstance and ModulePackage. Platform and TransformerRegistration apply and delete no cluster object and are not touched.
- Code: `internal/apply` (`prune.go`, `claims.go`, `apply.go`, `manager.go`), `internal/reconcile` (`moduleinstance.go`, `modulepackage.go`), `internal/status` (one reason), `api/v1alpha1/modulepackage_types.go`, `docs/site/`.
- Enhancement: implements part of 0012 (`enhancement.yaml`). No decision is claimed until both frontends have adopted both packages.
- RBAC: none. The verdict reads each object with the identity that deletes it, which already needs `get` for the prune's read today.
