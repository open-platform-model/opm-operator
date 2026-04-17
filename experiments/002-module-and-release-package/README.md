# Experiment 002: Module and Release in the Same CUE Package

## Background

The controller loads a `#Module` from an OCI artifact (via Flux source-controller),
but the render pipeline expects a `#ModuleRelease` — specifically, a concrete
`components` field that `#ModuleRelease` materializes from `#Module.#components`
via a for-comprehension.

Today the controller passes the raw `#Module` value directly to the render pipeline.
The `#Module` only has `#components` (a CUE definition), not `components` (a concrete
field). Result: `"no components field in release spec"` — every reconcile fails.

The CLI solves this by having the user write a `release.cue` that imports
`#ModuleRelease` and assigns `#module:`. CUE evaluation handles the materialization.
The controller has no release file.

### Why same-package injection does not work

Module files embed `m.#Module` at the root of the package:

```cue
package cert_manager

import m "opmodel.dev/core/v1alpha1/module@v1"

m.#Module

metadata: { ... }
#config:  { ... }
#components: { ... }
```

The root-level `m.#Module` constrains **all** top-level fields in the package.
Any file added to the same package — such as an injected `_release.cue` — must
satisfy the `#Module` schema. Fields like `components` and `values` are not part
of `#Module`, so CUE rejects them. Same-package injection is not viable.

## Hypothesis

> After Flux downloads and extracts a CUE module (`#Module`) to a local directory,
> the controller can restructure the directory — moving the module files into a
> subpackage and injecting a `module_release.cue` at the root — so that the release
> file imports the subpackage and materializes `#components` into a concrete
> `components` field.

The `cue.mod/` directory must remain at the root of the extracted directory or
CUE import resolution will fail.

If this works, the controller can:

1. Download + extract the `#Module` via Flux (as today)
2. Move the module's `.cue` files into a `module/` subdirectory (subpackage)
3. Inject a `module_release.cue` at the root that imports `"./module"`
4. Load the root package with `load.Instances`
5. Get a value with concrete `components`, `metadata`, and `values`
6. Feed it into the existing `ParseModuleRelease` → `ProcessModuleRelease` pipeline

If this does **not** work (CUE cannot import a subpackage from a restructured
directory), then Flux source-controller adds no value and the controller should
handle OCI pull and CUE loading directly.

## What This Tests

### Core question

Can the controller restructure a downloaded `#Module` into a subpackage layout
and inject a root-level release file that imports it — producing a concrete value
with `components`, `values`, and `metadata`?

### Sub-questions

1. Does moving `.cue` files into a `module/` subdirectory (with its own `package`
   declaration) while keeping `cue.mod/` at the root produce a valid CUE module
   layout?
2. Can a root-level `module_release.cue` import `"./module"` and access
   `#components`, `#config`, and `metadata` from the subpackage?
3. Does `for k, v in mod.#components { (k): v }` produce concrete struct fields
   when iterating over a definition from an imported package?
4. Does `values: mod.#config` bind the config defaults into a concrete `values`
   field that can later be overridden via `FillPath`?
5. Does the resulting root package pass `Validate(cue.Concrete(true))` when
   values are filled?
6. Can `LookupPath(cue.ParsePath("components"))` find the materialized field?
7. Do the definition fields (`#resources`, `#traits`) survive on each component
   inside `components` (needed for transformer matching)?
8. Does the `metadata` field from the subpackage remain concrete and decodable?

### What this does NOT test

- Full `#ModuleRelease` schema features (UUID generation, auto-secrets, policies,
  label merging). Those are catalog-level features that can be added incrementally.
- Cross-module import of `#ModuleRelease`. That is a separate approach — if this
  experiment succeeds, we may not need it.
- Controller reconciliation, apply, or prune behavior.
- Provider loading or transformer execution.

## Success Criteria

The experiment succeeds when:

1. Restructuring the module into a subpackage produces no CUE evaluation errors.
2. The injected `module_release.cue` imports the subpackage and materializes
   `components` via for-comprehension without errors.
3. `cue export` on the root package emits JSON with a concrete `components` field
   containing the module's component definitions.
4. A Go test using `load.Instances` + `LookupPath("components")` finds the
   materialized components.
5. `Fields()` iteration on the materialized `components` yields the expected
   component names and structure.
6. Definition fields (`#resources`, `#traits`) are accessible on each component
   within the materialized `components` (via `LookupPath` with `cue.Def()`).

The experiment partially succeeds if:

- Materialization works but definition fields are stripped (lossy). The controller
  would need a two-pass approach: schema components from `#components`, data
  components from `components`.

The experiment fails if:

- CUE rejects the import of the subpackage from the root.
- The for-comprehension over an imported `#components` definition does not produce
  concrete fields.
- `cue.mod/` at the root does not provide import resolution for the subpackage.
- The restructured layout is fundamentally incompatible with CUE's module system.

**If the experiment fails**, Flux source-controller adds no value to this pipeline
and the controller should handle OCI artifact fetching and CUE loading directly.

## Test Plan

### Phase 1: CUE CLI validation (no Go)

Use the existing `test/fixtures/modules/hello` module as the base.

#### Step 1: Copy and restructure the module

```bash
./experiments/002-module-and-release-package/scripts/setup.sh
```

This script:

1. Copies `test/fixtures/modules/hello/` to a temp directory.
2. Moves all `.cue` files (except `cue.mod/`) into a `module/` subdirectory.
3. `cue.mod/` stays at the root.

Resulting layout:

```
$WORKDIR/
├── cue.mod/
│   └── module.cue
├── module/
│   ├── module.cue
│   ├── components.cue
│   └── ...other .cue files
└── module_release.cue   (injected in Step 2)
```

#### Step 2: Inject the release materialization file

The script writes `module_release.cue` at the root:

```cue
package release

import mod "./module"

// Materialize #components (definition) into components (concrete field).
components: {
    for k, v in mod.#components {
        (k): v
    }
}

// Bind values to config defaults.
// The controller will override this via FillPath("values", merged) with
// user-supplied values from the ModuleRelease CR.
values: mod.#config

// Carry forward module metadata.
metadata: mod.metadata
metadata: namespace: string | *mod.metadata.defaultNamespace
```

#### Step 3: Validate with cue vet

```bash
cd "$WORKDIR" && cue vet ./...
```

Expected: no errors. The root package is schema-valid.

#### Step 4: Export with concrete values

```bash
cd "$WORKDIR" && cue export . -e 'close({components, metadata, values: mod.debugValues})'
```

Expected: JSON output with concrete `components` containing the module's
component definitions.

If `-e` filtering does not work cleanly with the import alias, try:

```bash
cue eval . -c
```

The `-c` flag requires concreteness — this verifies that `components` resolves
to concrete data.

#### Step 5: Check that components materialized

```bash
cue eval . -e 'components'
```

Expected: a struct with the module's component names and structure.

### Phase 2: Go API validation

#### Step 6: Go test — load and inspect

Write `experiments/002-module-and-release-package/main_test.go` that:

1. Copies the fixture to a temp dir.
2. Restructures: moves `.cue` files into `module/` subdirectory.
3. Writes `module_release.cue` at the root.
4. Loads the root package via `load.Instances`.
5. Asserts `LookupPath("components").Exists()` is true.
6. Iterates `components.Fields()` and asserts component names.
7. Checks `LookupPath(cue.MakePath(cue.Str("hello"), cue.Def("resources")))` exists
   on the schema-preserving value (before finalization).

#### Step 7: Go test — simulate controller FillPath

Extend the test to:

1. Compile a values JSON blob: `{"message": "test-value"}`.
2. Call `FillPath(cue.ParsePath("values"), compiled)` on the loaded value.
3. Assert the result is concrete.
4. Assert `components.hello.spec.configMaps.hello.data.message` equals `"test-value"`.

### Phase 3: Edge cases

#### Step 8: Package name detection

The injected `module_release.cue` needs a `package` declaration. The root package
name is chosen by the controller (e.g., `release`). The subpackage name must match
the original module's package declaration. Test that the package name can be
detected from the CUE instance metadata:

```go
instances := load.Instances([]string{"./module"}, cfg)
pkgName := instances[0].PkgName
```

#### Step 9: Concreteness without values

Load the root package without filling any values. Verify that:

- `components` exists (the comprehension ran)
- But `Validate(cue.Concrete(true))` fails (values not yet filled)
- This confirms the controller must fill values before concrete validation

#### Step 10: Overlay vs filesystem write

Test using `load.Config.Overlay` to inject `module_release.cue` as an in-memory
overlay instead of writing to disk:

```go
cfg := &load.Config{
    Dir: workDir,
    Overlay: map[string]load.Source{
        filepath.Join(workDir, "module_release.cue"): load.FromString(releaseCUE),
    },
}
```

If this works, the controller only needs to restructure the directory (move files)
but never writes the release file to disk.

## Key Risks

### CUE subpackage import resolution

The restructured layout relies on CUE resolving `import "./module"` relative to
the root where `cue.mod/` lives. If CUE's import system does not support relative
subpackage imports in this layout, the entire approach fails. This is the first
thing tested (Step 3).

### Package name mismatch

The subpackage inherits whatever `package` declaration the original module had
(e.g., `package cert_manager`). The root release file uses a different package
name (e.g., `package release`). CUE requires consistent package names within a
directory, but allows different names across directories. This should work but
must be validated.

### Definition iteration across import boundary

The for-comprehension `for k, v in mod.#components` iterates over a definition
from an imported package. CUE may behave differently when iterating definitions
across import boundaries vs within the same package. Tested in Step 5.

### Definition preservation across import

After materialization, each component inside `components` may or may not preserve
definition fields like `#resources` and `#traits`. If CUE strips definitions
during the for-comprehension or across the import boundary, the matching logic
would break. Tested in Step 6.

### Namespace field

The `#Module` schema has `metadata.defaultNamespace` (optional). The release
needs `metadata.namespace` (required). The injected file adds
`metadata: namespace: string | *mod.metadata.defaultNamespace` to bridge this.
If `defaultNamespace` is not set, this leaves `namespace` as `string`
(non-concrete) — the controller must fill it from the CR.

## Decision Gate

If the experiment **succeeds**: keep Flux source-controller. The controller
pipeline becomes download → restructure → inject → load → FillPath →
ParseModuleRelease → ProcessModuleRelease.

If the experiment **fails**: drop Flux source-controller. The controller handles
OCI artifact fetching and CUE loading directly, using a different approach to
bridge `#Module` → `#ModuleRelease`.

## Relation to Controller Design

If this experiment succeeds, the controller pipeline becomes:

```
Flux OCIRepository → extract #Module to /tmp
                            ↓
              restructure: move .cue files → module/ subdir
              (cue.mod/ stays at root)
                            ↓
              inject module_release.cue at root
              (overlay or filesystem)
                            ↓
              load.Instances → root package
                            ↓
              FillPath("values", crValues)
              FillPath("metadata.name", crName)
              FillPath("metadata.namespace", crNamespace)
                            ↓
              ParseModuleRelease (existing code, no changes)
                            ↓
              ProcessModuleRelease (existing code, no changes)
```

The only new code is the restructuring + injection step. Everything downstream
remains identical to the CLI's pipeline.

## Prerequisites

- `cue` CLI (v0.16+)
- Go 1.25+
- Access to the `test/fixtures/modules/hello` fixture (this repo)
- The fixture's CUE dependencies must be resolvable (registry or cached)
