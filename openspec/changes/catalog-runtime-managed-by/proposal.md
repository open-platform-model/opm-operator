## Why

The catalog's `#TransformerContext` hardcodes `app.kubernetes.io/managed-by: "open-platform-model"` in `controllerLabels`. Both runtimes (CLI and controller) try to override this via `#runtimeLabels`, but the CUE catalog never iterates `#runtimeLabels` into the final `labels` block. The Go-side injection is dead code, and every rendered resource carries the legacy literal regardless of which runtime produced it. There is no way to tell from a resource's labels whether it was applied by `opm-cli` or `opm-controller`, which complicates audit, prune ownership checks, and operator UX.

This change owns the catalog edits (cross-repo dependency tree: `catalog/` is consumed via OCI by both `cli/` and `poc-controller/`). The CLI-side runtime update is tracked as a parallel change in the CLI repo (`cli/openspec/changes/catalog-runtime-managed-by`); both runtimes pin to the new catalog version once published. Owning the catalog edit in one place avoids two runtime repos racing each other to edit the same shared schema.

## What Changes

- **Catalog schema (in `catalog/core/v1alpha1/transformer/transformer.cue`)**: Add a mandatory field `#TransformerContext.#runtimeName!: t.#NameType` (no default). Replace the hardcoded `controllerLabels["app.kubernetes.io/managed-by"]` value with a reference to `#runtimeName`. CUE evaluation FAILS at render time if the runtime forgets to inject. Remove the orphaned `#runtimeLabels` field — it is not used by the labels merge and the explicit field is clearer.
- **Catalog publish**: bump the catalog module version, publish to OCI registry. Both runtimes pin to the new version.
- **Controller render path (`internal/render/module.go`)**: Replace the existing `#runtimeLabels` `FillPath` (lines 94-100) with a `#runtimeName` fill set to `core.LabelManagedByControllerValue` (`opm-controller`). Same logic in `pkg/render/execute.go:243-259` is now the single injection point through `runtimeNameOverride`.
- **Spec hygiene (`pkg/core/labels.go`)**: Add a doc comment to `LabelManagedBy` and the value constants explicitly noting they MUST agree with the catalog's `controllerLabels` schema. Add a render-and-check unit test in `pkg/render/` that evaluates a minimal module and asserts that the rendered resource's `app.kubernetes.io/managed-by` value equals `core.LabelManagedByControllerValue`. This makes "Go-side and CUE-side agree on the key spelling and contract" a test-enforced invariant rather than a comment.
- **Optional follow-on cleanup (this change)**: The render-and-check test also confirms the orphaned `#runtimeLabels` field is gone (not just unused), preventing accidental reintroduction.

This is a PATCH change for `poc-controller` (no controller behavior change for users — `managed-by` value transitions from `"open-platform-model"` to `"opm-controller"`, which `core.IsOPMManagedBy` already recognizes). For external consumers matching by the legacy literal, see Impact below.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cue-rendering`: The `Runtime labels injection` requirement is broadened to "runtime identity injection" — runtime MUST fill the catalog's `#TransformerContext.#runtimeName` field; the catalog now treats this as mandatory and CUE evaluation fails if absent.

## Impact

- **Catalog (cross-repo)**: `catalog/core/v1alpha1/transformer/transformer.cue` adds `#runtimeName!`, removes `#runtimeLabels`, references `#runtimeName` in `controllerLabels`. Catalog version bumps; this change includes the publish step.
- **Controller code**: `internal/render/module.go` and `pkg/render/execute.go` both updated to fill `#runtimeName` instead of `#runtimeLabels`. Net diff: smaller (one field, not a map).
- **CLI repo (sister change)**: `cli/openspec/changes/catalog-runtime-managed-by` performs the equivalent rename in `cli/pkg/render/execute.go`. Both runtimes consume the same catalog version.
- **External consumers**: anyone matching resources by `app.kubernetes.io/managed-by=open-platform-model` will need to accept `opm-cli` or `opm-controller`. Internal consumers via `core.IsOPMManagedBy` are unaffected.
- **API**: No CRD changes.
- **SemVer**: PATCH for the controller; the catalog bump is a MINOR (additive required field counts as schema break for any custom transformer using `#TransformerContext` directly — those must inject `#runtimeName`; in practice all transformers run through the catalog's `controllerLabels` and a runtime always fills the field, so end users are unaffected).
- **Sequencing**: catalog must publish before either runtime change merges. Tasks in this change cover the catalog edit + publish; the CLI sister change waits on catalog publish then ships.
