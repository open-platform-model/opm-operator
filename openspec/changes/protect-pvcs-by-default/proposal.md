## Why

The operator deletes a PersistentVolumeClaim like any other tracked object: when `spec.prune` is true, a claim that a new render drops is pruned, and deleting the ModuleInstance deletes every tracked claim. The prune exempts only Namespaces and CustomResourceDefinitions (`internal/apply/prune.go`, `isSafeToDelete`). Deleting a claim deletes the data on its volume under the usual reclaim policy, and nothing brings it back.

The CLI stopped doing this in cli#345: `opm instance delete` and the prune of apply keep claims unless `--delete-data` is passed. The two managers now differ, and a claim that a CLI prune kept stays in an inventory that an operator takeover can prune. The owner decided on 2026-10-08 ("Protect in the operator too") that the operator gets the same default before v1.0.0, with an explicit opt-out on the ModuleInstance.

## What Changes

- **BREAKING** (changed default): with `spec.prune: true`, the operator no longer deletes a PersistentVolumeClaim of the core API group, neither when it prunes a stale claim nor when the ModuleInstance or ModulePackage is deleted. The claim and its data stay in the cluster.
- A new optional field is the opt-out. It is `spec.dataPolicy`, an enum with the values `Keep` and `Delete`, on ModuleInstance and on ModulePackage; absent means `Keep`. The owner chose this shape at the proposal gate on 2026-10-08, with no validation rule that ties it to `spec.prune`. `design.md` records the two shapes that were not chosen.
- No existing field changes meaning. `spec.prune` still decides whether the operator deletes anything at all; the new field only decides whether claims are among what it deletes. With `spec.prune` false or absent the new field has no effect.
- A kept claim never blocks anything: the reconcile succeeds, the finalizer is removed, the deletion completes. The operator reports kept claims with one `Normal` event (reason `ClaimsKept`) that names them, and a log line.
- A stale claim that the prune keeps leaves `status.inventory`, as every stale entry does today when it is skipped or not pruned. From then on nothing tracks it; it is deleted with `kubectl delete pvc`.
- Claims that a StatefulSet creates from its `volumeClaimTemplates` are not in any inventory. The operator never deleted them and still does not, with or without the new field. The docs say so.
- Docs: the two operating pages, the conditions and events page, the resource reference (generated from the doc comment), a new ADR beside ADR-011, and the release note.

Not in this change: the CLI (its prompt and docs page for operator-managed instances need a follow-up, see `design.md`, "Handover between the CLI and the operator"); other kinds than PersistentVolumeClaim; any restore step; a status field that lists kept claims.

SemVer: MAJOR after GA, because a default that deletes becomes a default that keeps. During beta it ships as the next `1.0.0-beta.N` with a `!` in the PR title: `feat(apply)!: keep PersistentVolumeClaims on prune and deletion unless spec.dataPolicy is Delete`.

Complexity (Principle VII): one two-value field, one branch in the prune, one event. The alternative is to tell users that `spec.prune` destroys data, which the owner rejected.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `prune-stale-resources`: the prune keeps core PersistentVolumeClaims unless the opt-out is set, reports what it kept, and the opt-out field is defined.
- `finalizer-and-deletion`: deletion cleanup with prune enabled keeps claims by default, and a kept claim does not hold the finalizer.
- `events-emission`: a `ClaimsKept` event on the prune path and on the deletion path.
- `modulepackage-reconcile-loop`: ModulePackage follows the same rule with the same field.

## Impact

- API: `api/v1alpha1` gains one optional field on `ModuleInstanceSpec` and `ModulePackageSpec`. Generated: both CRDs in `config/crd/bases`,  `modules/opm_operator/zz_generated_crds.cue`. The last one means the squash commit also lands in the operator module's changelog and opens a module release PR, which its release gate holds until an operator release carries the new `config/` (AGENTS.md, "This repository releases two units").
- Code: `internal/apply/prune.go` (the guard and the result), the four `apply.Prune` call sites in `internal/reconcile/moduleinstance.go` and `internal/reconcile/modulepackage.go`, `internal/status/conditions.go` (one event reason).
- Tests: `internal/apply` unit tests, `test/integration/reconcile` (prune and deletion, both kinds), `test/integration/crdvalidation`.
- Docs: `docs/site/operating/deletion-and-pruning.md`, `docs/site/operating/delete-an-instance-safely.md`, `docs/site/diagnostics/operator-conditions.md`, `adr/020-*.md`.
- Users: an instance with `spec.prune: true` that relied on the operator to delete claims must set the new field. Nothing else changes for them.
- CLI: no code dependency. The CLI's confirmation prompt for an operator-managed instance with `spec.prune` set says the operator deletes claims; after this change that is true only when the new field is set. The follow-up is a CLI change of its own (swarm task T9.26); this change does not wait for it.
