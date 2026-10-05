## Context

Both workload status types carry four `lastApplied*` fields: `lastAppliedAt` and the source, config and render digests. The deferred status commit of each reconcile loop sets them when the attempt applied (and pruned) successfully:

```go
// internal/reconcile/moduleinstance.go, ReconcileModuleInstance (after #236)
if reconciled {
	mi.Status.LastAppliedAt = &now
	mi.Status.LastAppliedSourceDigest = digests.Source
	mi.Status.LastAppliedConfigDigest = digests.Config
	mi.Status.LastAppliedRenderDigest = digests.Render
	mi.Status.Inventory = nextInventory(mi.Status.Inventory, newEntries)
	...
}
```

`ReconcileModulePackage` has the same block. The source digest is the only trace of the module version: `ModuleSourceDigest(path, version)` hashes `spec.module.path@spec.module.version` for a ModuleInstance, and a ModulePackage stores the Flux artifact digest, which names no module version.

The version is available at render time on both paths. Each kernel renderer holds a `*module.Instance` whose `Package` is the concrete instance value, and the source module sits at `schema.Module` (`#module`) with its metadata at `schema.Metadata` (`metadata`). Core v2 requires `metadata.version` to be concrete; the test fixtures set it from their identity package as a bare SemVer (`test/fixtures/modules/hello/identity/identity.cue`: `Version: "0.1.0"`).

Reconcile phase impact: Render reports one more fact (the module version). Status gains one field, written on success and on a NoOp that finds it empty. Source, Apply and Prune are untouched, and no-op detection compares the same four digests as before.

## Goals / Non-Goals

**Goals:**

- `status.lastAppliedVersion` names, in plain text, the version of the module whose render the operator last applied, on ModuleInstance and ModulePackage.
- It moves only when the `lastApplied*` digests move, plus a one-time fill on NoOp for objects applied before the field existed.
- It means the same on both kinds.

**Non-Goals:**

- A plaintext record of the module path, the render, or a stored plan. The owner's decision names the version only; ADR-008 (as amended by library#167) builds deletion plans from the inventory plus live objects.
- Transition detection (install, upgrade, reconfigure). The field is an input a later change may read; nothing here compares it.
- The cli writing the field for CLI-owned instances. The owner's decision says the operator adds it; a cli counterpart would be its own change.
- A `kubectl get` print column. The ModuleInstance already prints `spec.module.version`.
- Reading the version through a library accessor. The Instance module-metadata accessor of lib-i3d2 is not merged; the `opm/schema` path variables used here are public library API today.

## Research & Decisions

### 1. Record the module's own `metadata.version`, not `spec.module.version`

**Context**: A ModuleInstance names its version in `spec.module.version`, so the reconciler could copy that string. A ModulePackage has no such field; its module version is wherever the package's `instance.cue` points.

**Explored**: `spec.module.version` accepts both `v0.1.0` and `0.1.0` (library i1, accept-bare-semver). Copying it would record the claim spelling, the same inconsistency opm-operator#234 records for `status.registry[].version`. The rendered instance carries `#module.metadata.version` on both kinds, read the same way.

**Decision**: Both renderers read `inst.Package.LookupPath(schema.Module).LookupPath(schema.Metadata).LookupPath(cue.ParsePath("version"))` and report it as `RenderResult.ModuleVersion`. The status doc comment says it is the version as the module declares it, bare SemVer.

**Rationale**: One source and one spelling for both kinds; it is the version that was actually rendered, not the version that was asked for. "Plain text" in the owner's words is satisfied either way, and this reading avoids a second #234.

```go
// internal/render/version.go (sketch)
var moduleVersionPath = cue.ParsePath("version")

// declaredModuleVersion returns the version the instance's source module declares,
// or "" when it cannot be read as a concrete string.
func declaredModuleVersion(inst *module.Instance) string {
	if inst == nil {
		return ""
	}
	v := inst.Package.LookupPath(schema.Module).LookupPath(schema.Metadata).LookupPath(moduleVersionPath)
	s, err := v.String()
	if err != nil {
		return ""
	}
	return s
}
```

### 2. An unreadable version never fails the reconcile

**Context**: The version is informational. Core and the kernel's loader gate already refuse a module whose metadata is not concrete, so an empty read should not happen.

**Decision**: `declaredModuleVersion` returns `""` on any read failure, and the reconcile proceeds. On a successful apply the field is then written empty (and omitted from the object by `omitempty`), so a stale version never survives an apply of a different module.

**Rationale**: Failing a reconcile over a status annotation would turn a cosmetic gap into an outage. Writing empty rather than keeping the old value keeps the invariant "the field names what the last apply rendered, or nothing".

### 3. NoOp fills an empty field and never overwrites one

**Context**: The NoOp commit is bounded ("Status always patched" in `reconcile-loop-assembly`): `lastAttempted*`, `inventory` and history are never touched on NoOp. `lastApplied*` is not in that forbidden list, and `requiredContracts` is already written on NoOp because it is a fact about the render. After an upgrade to an operator with this field, every object that does not change stays NoOp, so a write on apply alone leaves the field empty on all existing objects indefinitely.

**Explored**: (a) Write only on apply: simplest, but the field is empty for the objects that matter most, the long-lived steady ones. (b) Write on every NoOp: a NoOp could then rewrite a recorded value. (c) Write on NoOp only when empty.

**Decision**: (c). When the outcome is NoOp and `status.lastAppliedVersion` is empty, the NoOp commit sets it to the version the render just reported. ModuleInstance: in `commitNoOpStatus` (or just before it, where the render result is in scope). ModulePackage: in the inline NoOp branch of the deferred func.

**Rationale**: A NoOp means the source, config, render and inventory digests all equal the last apply's. The source digest pins the module version on both kinds: `path@version` for a ModuleInstance, and for a ModulePackage the artifact digest covers `instance.cue` and `cue.mod`, which pin the module dependency. So the version this render reports is the one that was last applied, and filling the field is a correct backfill, not a guess. Restricting it to an empty field means a NoOp never changes a recorded value.

### 4. Two sections

**Context**: Principle VIII wants each section green and releasable.

**Decision**: Section 1 adds `RenderResult.ModuleVersion` and fills it in both renderers, with tests; nothing reads it yet, so `main` behaves as before. Section 2 adds the API field, the writes in both loops, the generated files and the envtest specs.

**Rationale**: The render half can be reviewed and tested against the real kernel on its own. The API half is the user-visible part and carries every generated file in one commit, which the `operator-module:drift` gate requires.

## Risks / Trade-offs

- [Overlap with the dogfood session] `modules/opm_operator/zz_generated_crds.cue` is also regenerated by other branches that touch CRDs (op-g3, op-e4, op-f5 later in this wave, and any dogfood-module work). → Regenerate with `task dev:manifests`, never hand-merge; on a conflict, take either side and rerun the task. Report the overlap in the PR body.
- [Module release side effect] The CRD regeneration lands in the module's changelog and opens a module release PR. → Expected and documented in `AGENTS.md`; the module's release gate holds it until an operator release carrying this `config/` exists.
- [Empty on CLI-owned instances] The cli does not write the field, so a CLI-owned ModuleInstance shows it empty. → Stated in the field's doc comment ("set by the operator"); a cli counterpart is out of scope.
- [Version not a no-op input] Two different modules at the same version could in theory differ only in content; that is the render digest's job, unchanged here.
