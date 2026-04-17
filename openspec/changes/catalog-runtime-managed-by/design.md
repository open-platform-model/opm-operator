## Context

The catalog's `#TransformerContext` (defined in `catalog/core/v1alpha1/transformer/transformer.cue:92-184`) is the single source of truth for what labels appear on every rendered resource. The `labels` block at lines 160-173 merges three subsets — `moduleLabels`, `componentLabels`, `controllerLabels` — into a final flat map. Today:

- `controllerLabels["app.kubernetes.io/managed-by"]` is hardcoded to the literal `"open-platform-model"` (line 155).
- A separate field `#runtimeLabels` exists (filled by both runtimes via `cue.ParsePath("#runtimeLabels")` `FillPath` calls) but is **never iterated into the `labels` merge**. The mechanism is dead code.

Result: every resource — whether applied via `opm-cli` or `opm-controller` — carries `managed-by=open-platform-model`. The runtime identity is invisible. The Go-side injection (`internal/render/module.go:94-100`, `pkg/render/execute.go:243-259`) is wasted work.

## Goals / Non-Goals

**Goals:**

- Make runtime identity authoritative on every rendered resource. CLI-rendered resources carry `managed-by=opm-cli`; controller-rendered resources carry `managed-by=opm-controller`.
- Make the runtime identity injection mandatory at the CUE schema level — render fails fast if a runtime forgets, rather than silently stamping a wrong value.
- Eliminate the dead `#runtimeLabels` mechanism and replace it with an explicit `#runtimeName` field that maps directly to `controllerLabels["app.kubernetes.io/managed-by"]`.
- Lock the contract between Go-side label constants (`pkg/core/labels.go`) and CUE-side schema with a render-and-check test.

**Non-Goals:**

- Adding new label keys to the catalog. Out of scope; this change is purely a refactor of the runtime-identity injection mechanism.
- Migrating existing in-cluster resources whose `managed-by` is the legacy `"open-platform-model"` value. `core.IsOPMManagedBy` accepts the legacy value and continues to recognize them; no relabel campaign needed.
- Touching `moduleLabels` or `componentLabels` in the catalog. Their semantics are unchanged.
- Coordinating the CLI's runtime update — that lives in the CLI sister change. This change publishes the catalog and updates the controller; the CLI change pins to the new catalog version and updates its render path.

## Decisions

### Decision 1: Mandatory `#runtimeName` field (option voted by user)

**Choice**: Add `#runtimeName!: t.#NameType` to `#TransformerContext`. Reference it from `controllerLabels`:

```cue
controllerLabels: {
    "app.kubernetes.io/managed-by": #runtimeName  // required, no default
    "app.kubernetes.io/name":       #componentMetadata.name
    "app.kubernetes.io/instance":   #componentMetadata.name
}
```

The `!` makes the field required. CUE evaluation fails with a clear error if a runtime evaluates the schema without filling `#runtimeName`.

**Why**: explicit, type-safe, fail-fast. Runtimes can't accidentally render with an empty or unset value — CUE catches it at evaluation time. The field is named for its purpose (`#runtimeName` = identity of the rendering runtime), not generically (`#runtimeLabels` was a bag of overrides without a clear contract).

**Alternatives considered**:

- *Wire `#runtimeLabels` into the labels merge*: keeps the existing field but couples a runtime-identity feature to a generic override mechanism. The override semantics are confusing (does it shadow component labels too? what about precedence?). Reject in favor of a single-purpose field.
- *Default to `"unknown"` and log a warning*: silent misconfiguration. The whole point is fail-fast.
- *Make `#runtimeName` optional with a default `"open-platform-model"` (legacy)*: preserves backward compat for any out-of-tree consumer of `#TransformerContext` but defeats the goal of making runtime identity authoritative. Reject.

### Decision 2: Remove `#runtimeLabels` entirely

**Choice**: delete the `#runtimeLabels` field declaration from `#TransformerContext`. Both runtimes' `FillPath` calls move from `#runtimeLabels` (a struct) to `#runtimeName` (a string).

**Why**: dead code accumulates. Leaving the field in place "for future use" invites the next developer to repeat the same orphaning bug or to wire it incorrectly. Deletion forces a new use case to design itself explicitly, with proper merge semantics in the `labels` block.

**Risk**: any out-of-tree consumer that fills `#runtimeLabels` (none known) will get a CUE evaluation error. Acceptable — the field never did anything anyway.

### Decision 3: Catalog version bump as part of this change

**Choice**: include the catalog publish (version bump + push to OCI) as a task in this change, even though catalog code lives in a sister repo. Bump scheme: MINOR (the new mandatory field is a schema break for any code that constructs `#TransformerContext` directly without a runtime, but the catalog ships transformers, not raw `#TransformerContext` instances; in practice no end user is broken).

**Why**: avoids the dependency-ordering ambiguity that would otherwise force three changes (catalog publish → controller change → CLI change) to coordinate via PR comments. Bundling the catalog edit + publish here means the controller change can pin to the new version atomically. The CLI sister change waits on this change merging, then pins.

**Alternatives considered**:

- *Three separate changes with explicit dependencies*: more ceremony, more chances for misordering. Reject.
- *Only edit the catalog locally without publishing*: leaves the catalog repo and registry diverged. Reject.

### Decision 4: Render-and-check test as the contract enforcer

**Choice**: add a unit test in `pkg/render/` (or `internal/render/`) that:

1. Constructs a minimal `#ModuleRelease` (one component, one resource).
2. Renders it through the same pipeline used by the controller, with `#runtimeName` filled to `core.LabelManagedByControllerValue`.
3. Asserts that the resulting resource's `metadata.labels["app.kubernetes.io/managed-by"]` equals `core.LabelManagedByControllerValue`.
4. Asserts the rendered resource also carries `module-release.opmodel.dev/uuid` (sanity check that the catalog continues to stamp ownership labels).

**Why**: catches drift between the Go constant and the CUE schema. If someone renames `LabelManagedBy` in Go without updating the catalog, the test fails. If the catalog regresses to hardcoding a literal again, the test fails. Single test, two-way contract enforcement.

**Alternatives considered**:

- *Generated golden file*: brittle to whitespace and CUE evaluator output ordering. Reject.
- *Cross-repo CI check*: requires CI infra changes. Defer; the in-process render test is sufficient.

### Decision 5: No status/migration logic for existing resources

**Choice**: existing in-cluster resources with `managed-by=open-platform-model` (legacy) keep that label. `core.IsOPMManagedBy` already recognizes it (`pkg/core/labels.go:48`), so prune ownership guards continue to work. New renders produce the new value. SSA apply will eventually update the label on next render of any given resource, no explicit migration needed.

**Why**: SSA's natural behavior covers the migration. Adding a one-shot relabel job is unnecessary complexity and risks bugs.

## Risks / Trade-offs

- **Risk**: out-of-tree CUE consumers (third-party transformers, custom catalog extensions) that construct `#TransformerContext` directly will fail to evaluate after the new mandatory field lands. → **Mitigation**: document in the catalog release notes; the failure mode is loud and immediate (CUE error at evaluation), not silent corruption. Migration is one line: fill `#runtimeName` with whatever identity is appropriate.
- **Risk**: external monitoring/alerting that matches `managed-by=open-platform-model` will stop seeing newly-rendered resources. → **Mitigation**: change is announced in release notes. Internal tooling uses `core.IsOPMManagedBy`, which accepts both old and new values. External consumers update at their own cadence (the legacy value is still honored for already-stamped resources).
- **Risk**: catalog publish + controller change must land in order, or the controller breaks against the old catalog. → **Mitigation**: tasks enforce ordering (publish first, pin in controller second). The CLI sister change has its own pin task.
- **Risk**: the new mandatory field could cause CUE evaluation to fail in test fixtures that don't set `#runtimeName`. → **Mitigation**: any test rendering through the production pipeline already goes through `internal/render/module.go` (or `pkg/render/execute.go`), which will fill the field. Tests that bypass the production pipeline by hand-constructing `#TransformerContext` (none currently known in `poc-controller`) need to fill the field; design test will surface them.

## Migration Plan

1. Catalog edit + publish (this change, tasks 1.x).
2. Pin `cue.mod/module.cue` in `poc-controller/` to the new catalog version (this change, task 2.x).
3. Update controller render code (this change, tasks 3.x).
4. Render-and-check test added (this change, task 4.x).
5. Validation gates (this change, task 5.x).
6. CLI sister change pins to the same catalog version and updates `cli/pkg/render/execute.go` independently.

Rollback: revert this change's commits. The catalog can stay published — the new field is schema-additive from the catalog's perspective once any runtime fills it; older runtimes ignoring it would fail evaluation, which is intended fail-fast behavior. If urgent rollback is needed, publish a catalog hotfix that makes `#runtimeName` optional with the legacy default.

## Open Questions

- Catalog version bump: 0.1.0 → 0.2.0 (MINOR) or smaller? Confirm with catalog maintainer convention before publishing. The catalog has its own versioning policy that this change should respect.
- Whether to also delete the `controllerLabels["app.kubernetes.io/instance"]` while we're touching this code. It currently equals component name, same as `app.kubernetes.io/name` — duplicate. Out of scope for this change unless the user asks.
