## Why

The operator judges every prune and delete with the library's ownership verdict (opm-operator#271), but its apply still asks no ownership question: `apply.Apply` hands the rendered set to Flux with forced field ownership (`internal/apply/apply.go:97-110`), so an instance takes over any object its render names, whoever holds it. The cli closed the same gap in cli#349. Enhancement 0012:D8 binds both frontends: the apply guard runs on every apply, and the `opmodel.dev/adopt` annotation is its only override.

## What Changes

This change is the apply half of the operator's ownership adoption. It needs no API or CRD change: no spec field, no status field, no flag.

- **BREAKING** Every reconcile that renders, for a ModuleInstance or a ModulePackage, asks `ownership.CanApply` for every object of its apply list, on a live read, before its first write.
- **BREAKING** A reconcile is refused as a whole, with nothing written, when the verdict refuses an object it would write, or an object that exists and is not in `status.inventory`. The reasons are the library's: `terminating`, `foreign-object`, `other-instance`. The object reports `Ready=False` with the new reason `ApplyRefused`, not `Stalled`, emits one `Warning` event `ApplyRefused`, and retries on the bounded backoff.
- **BREAKING** An object whose adopt annotation names another instance (`adopted-elsewhere`) is let go: it is not applied, not checked for drift, not restored, and it leaves `status.inventory`. The rest of the instance is applied and the object stays `Ready=True`. The message of `Ready=True` counts such objects, and one `Warning` event `AdoptedElsewhere` names them when the count changes.
- An existing object annotated `opmodel.dev/adopt=<this instance's UUID>` is applied and recorded, also when OPM did not manage it or another instance held it (0012:D8:R2).
- The guard asks with the instance's identity, and while an identity change is not settled also with the earlier one. It never asks with an empty identity.
- **BREAKING** A ModulePackage reconcile that renders and cannot build its client or read an object fails, also when its digests match. Today that is a silent no-op.
- With `spec.rollout.forceConflicts`, an object the instance takes in is never deleted and created again; an immutable difference fails the apply with nothing written.
- A read that fails for a reason other than "not found" stops the reconcile before any write.
- A test keeps the list of places that write a cluster object closed, as the delete half did for deletes.

### What the brief asked, in short (detail and evidence in `design.md`)

1. **Where the operator writes today.** One call writes cluster objects: `rm.ApplyAllStaged` (`internal/apply/apply.go:110`), reached from `applyInstance` (`internal/reconcile/moduleinstance.go:1559`, called at `:621`) and from `applyAndPruneModulePackage` (`internal/reconcile/modulepackage.go:764`). It carries the changed render, the restore of missing objects (`moduleinstance.go:587`), the Namespace and CRD stage and the forced recreate. No ownership check guards it. The checks that exist are of another kind: the claim rule (`apply.go:103-106`, `claims.go:70`), the withheld registration (`moduleinstance.go:532`), the identity refusal (`moduleinstance.go:516`) and the UID precondition of a forced recreate (`claims.go:161-177`).
2. **The verdict per case.** See the table "Verdict and action per case" in `design.md`. In short: no live object, ours, ours by the earlier identity, an OPM object without a UUID label, and one annotated for this instance are applied; a foreign object, another instance's object and a terminating object refuse the reconcile; an object annotated for another instance is let go.
3. **Whole reconcile or one object.** The whole reconcile is refused before any write. The inventory is not touched by a refused reconcile. Only `adopted-elsewhere` skips one object and goes on, as 0012:D8:R8 requires.
4. **The adopt annotation that names the earlier identity.** The operator does not rewrite it. While the change is not settled the guard accepts it through the earlier identity; on the healthy path that is one reconcile. After that the object is let go, and the event prints the annotation to set. Ruled at the supervisor's gate on 2026-10-09: no rewrite, no revision of 0012:D8:R6, no library change.
5. **A failed read.** Fail safe: nothing is written by that reconcile.
6. **Cost.** No extra read for a ModuleInstance: the guard's read replaces the read drift detection makes today (`internal/apply/drift.go:63`, `:102-109`). One GET per rendered object for a ModulePackage on a reconcile that renders. A reconcile that skips its render reads nothing.
7. **Breaking.** Yes, `feat!`. The list of clusters that see a new refusal, and the way out, is in `design.md` and in the migration note.
8. **API or CRD change.** None.

Not in this change: the delete side (merged); the deletion protocol, finalizers, deletion order and propagation (the change that adopts `opm/k8s/lifecycle`); RBAC; a way for the operator to set the adopt annotation (0012:D8:R6 excludes it); a status field that lists let-go objects; the cli.

SemVer: MAJOR after GA. During beta it ships as the next `-beta.N` under a `feat!` title, in the same operator release as the delete half.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ssa-apply`: an apply is judged by the library's apply verdict before its first write; the read is made as the identity that applies.
- `reconcile-loop-assembly`: a refused reconcile writes nothing; a let-go object leaves the inventory; a rendered object that exists outside the inventory is taken in by a restore and is never a no-op.
- `drift-detection`: a let-go object is left out of drift detection and of the restore; the restore step also applies an allowed object that exists outside the inventory, which is left out of the drift diff of that reconcile.
- `modulepackage-reconcile-loop`: a ModulePackage guards every reconcile that renders, as a ModuleInstance does.
- `status-conditions`: the reason `ApplyRefused`; the count of let-go objects in the message of `Ready=True`.
- `events-emission`: the `ApplyRefused` event and the `AdoptedElsewhere` event.
- `kubernetes-tier-adoption`: the operator decides every apply only through the library's ownership package, and the list of write call sites is closed.

## Impact

- API: none. No CRD, sample, generated file or operator module data changes.
- Controllers: ModuleInstance and ModulePackage. Platform and TransformerRegistration write status and finalizers of their own kinds only and are not touched.
- Code: `internal/apply` (a new guard, `drift.go`, the call-site test), `internal/reconcile` (`moduleinstance.go`, `modulepackage.go`), `internal/status` (one `Ready` reason, two event reasons and their notes), `docs/site/operating/`.
- Enhancement: implements part of 0012 (`enhancement.yaml`). No decision is claimed until the operator has also adopted the deletion protocol.
- RBAC: none for the operator's own role. A tenant ServiceAccount that may patch a kind and not `get` it can apply today and is refused after this change.
- cli: none. The cli already drives an operator-owned instance through its record only.
- Complexity (Principle VII): one function in `internal/apply` and one pass in each reconciler. No new package, no new dependency: `opm/k8s/ownership` is already imported at the pinned library v1.0.0-beta.7.
