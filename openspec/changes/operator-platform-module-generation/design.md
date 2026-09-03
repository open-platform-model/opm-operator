# Design: operator-platform-module-generation

## Context

See `proposal.md` § Why. This design has had its second pass: the six questions the first pass recorded are resolved below, each with the measurement or precedent that settled it. Current state: `PlatformReconciler` decodes the CR spec into `synth.PlatformInput`, calls `SynthesizePlatform` + `Materialize`, and stores the `*MaterializedPlatform` in the single-slot `internal/platform` store behind the kernel gate (the 0019 stopgap).

## Goals / Non-Goals

**Goals**

- The CR remains the operator's API; the platform module is derived, disk-local, operator-owned state (owner decision: the operator writes to disk for itself; nothing here lands in the kernel).
- Every failure a bad CR can cause surfaces on the CR's Ready condition with the offending path or entry named.

**Non-Goals**

- Consuming the module (render switch, store reshaping, kernel-gate retirement): `operator-render-switch`.
- Skew policy surface (0019 D7/D18): decided in the render-switch change.
- Publishing the module anywhere, in any form (D6 disallows it outright).

## Research & Decisions

### The operator writes the module to its own disk

**Context**: D5 makes the platform a module; something must own its bytes.
**Explored**: 0019 D6's alternatives (CR carrying CUE text, CR referencing a published platform module) were rejected in the enhancement; the remaining split was generate-in-memory-per-use versus generate-to-disk.
**Decision**: generate to the operator's own filesystem, once per CR generation, and validate there (owner decision, this change's mandate).
**Rationale**: the render build consumes directory replacements served from disk; a persistent per-generation module makes regeneration observable, diffable in support bundles, and shared across the render paths without re-deriving. Disk is ephemeral pod state and the CR is the source of truth, so loss is repaired by regeneration (see "Disk layout and lifecycle").

### Generation replaces synthesis; the CR shape does not move

**Context**: `SynthesizePlatform` is deleted by the library wave; the CR is a stable API.
**Decision**: the reconciler maps `spec.registry` to the module template (the same shape `cli-config-platform-module` establishes for the CLI seed) with the CR's `version` values used twice: as the `cue.mod` catalog pins and as the stamped expected `version` on each entry (the D13 tripwire).
**Rationale**: D6 verbatim. The double use is not two answers: the stamp unifies with the derived readout, so its only outcomes are agreement or a build conflict naming the entry.

Measured 2026-09-03 against the published artifacts (core `v2.0.0-alpha.7`, `catalogs/opm` `v4.0.1`, `catalogs/k8s` `v1.0.0-alpha.2`, CUE `v0.17.1`):

- A stamp that disagrees with the pinned bytes fails as `#registry."opmodel.dev/catalogs/opm@v4".version: conflicting values "4.0.1" and "4.0.0"`: the entry is named, the build refuses.
- A key that disagrees with the imported catalog fails as `#registry."opmodel.dev/catalogs/k8s@v2".#catalog.metadata._ref.modulePath: conflicting values "opmodel.dev/catalogs/k8s@v1" and "opmodel.dev/catalogs/k8s@v2"`.
- A pin naming an unpublished build fails as `opmodel.dev/catalogs/opm@v4.9.9: module not found`: path and version are in the message.
- An entry generated with `enable: false` builds cleanly and reads back `enable: false` with its `version` derived.

### Validation is a real build through the kernel loader

**Context**: a generated module can be wrong three ways: bad pin, key/import drift, generation defect.
**Decision**: after writing, build via the kernel's shape-gated platform loader against the operator's `--registry` mapping; Ready reflects the outcome.
**Rationale**: the build is the only check that proves the pins resolve and exercises the schema's own tripwires; anything less re-implements them.

**Library gate**: the operator pins library `v1.0.0-alpha.23`, which carries `Kernel.LoadPlatformPackage` (the shape-gated loader) but not `Kernel.AcquirePlatformFromDir` or `platform.Platform.Source` (both landed after alpha.23; the `1.0.0-alpha.24` release is pending). This change therefore builds through `LoadPlatformPackage` + `NewPlatformFromValue` and records the module directory itself; `operator-render-switch` adopts `AcquirePlatformFromDir` when it bumps the library for `Kernel.Render`. Nothing in the generated module or the store record changes shape at that point.

### The dependency list is the full MVS closure, derived from published module files (was OQ1)

**Context**: D13 promotes the platform module's dependency list WHOLE into the render module's roots and never runs a tidy at render time; "tidying happens once, at platform-package generation". 0019 experiment 02 measured that a default-major dependency (`cue.dev/x/k8s.io`) is honoured only as a root entry.
**Measured** (2026-09-03, CUE `v0.17.1`, scratch module pinning only core + the two catalogs):

- The roots-only module builds cleanly. CUE `v0.17.1` resolves an unqualified import inside a dependency package through the importing module's own default majors (`modpkgload.Packages.load`, `DependencyDefaultMajorVersion`) and then the module graph, so experiment 02's root-only finding no longer holds for the *platform's own* build.
- `cue mod tidy --check` reports the roots-only module untidy; `cue mod tidy` adds `"cue.dev/x/k8s.io@v0": v: "v0.10.0"` (no `default` marker: the platform imports nothing unqualified) and leaves the three roots as pinned.
- The render module is where the closure matters: promotion copies the platform's list verbatim (`renderstage.Promote`), so a path absent from the platform's list is resolved from the module graph by maximum-version selection across the instance's requirements too. For `cue.dev/x/k8s.io` that is exactly the authority-by-omission cell experiment 02 lost. The coverage invariant (`renderstage.VerifyCoverage`) checks OPM-namespace paths only, so nothing downstream catches it.

**Options considered**:

1. Roots only. Builds today, but hands every non-OPM path in the render build to maximum-version selection over the instance's pins; contradicts D13's "every path, `cue.dev/x/k8s.io`'s default-major trap included".
2. A tidy-equivalent on the reconcile path. `modload.Tidy` is `cuelang.org/go/internal`, not importable; shelling out to `cue` puts a binary in the image for one cold-path call.
3. Derive the closure from the pinned modules' own published module files: breadth-first over `modconfig.Registry.ModFile` from the roots, maximum version per major-qualified path (the roots participate in the maximum), derived entries carrying no `default` marker. This is minimum version selection computed the way `cue mod tidy` computes it, minus the prune of modules no import reaches (an unreachable extra root pins a path nothing evaluates; harmless, and consistent with the platform winning every path it lists).

**Decision**: option 3, in the operator's generation package. Registry I/O is confined to module-file fetches through the CUE module cache (`CUE_CACHE_DIR`), the same artifacts the validation build fetches anyway, once per CR generation (cold path). Roots are core (see the core-pin decision) and every subscribed catalog, disabled ones included: a disabled entry still imports its catalog.
**Shared with the CLI**: `cli-config-platform-module`'s seed is hand-written and the user tidies it (`cue mod get` / `cue mod tidy`, then `opm config vet`); its template carries the same closure shape. When `cli-platform-cr-generation` generates a module from a cluster CR it needs exactly this derivation; lifting the generator (template plus closure) into a library helper at that point is the right move, and is recorded there rather than pre-empted here (owner decision: nothing from this change lands in the kernel).

### Disk layout and lifecycle (was OQ2)

**Context**: the manager runs with `readOnlyRootFilesystem: true`; `/tmp` is an `emptyDir` already hosting the CUE module cache (`--cue-cache-dir`, default `/tmp/cue-cache`).
**Decision**:

- A `--platform-dir` manager flag (default `/tmp/opm-platform`), sibling of the CUE cache on the same `emptyDir`. No manifest change: the volume exists.
- One directory per CR generation: `<platform-dir>/gen-<generation>/{cue.mod/module.cue,platform.cue}`. Generation writes into `<platform-dir>/.staging-<generation>-<random>` and renames it into place; an existing `gen-<generation>` (a re-reconcile of the same generation after a build failure or a container restart on the same volume) is moved aside first and removed after the swap. A reader never observes a partially written module: the directory is either absent or complete.
- Retention: after a successful build the current and the immediately superseded generation are kept; older `gen-*` directories and every `.staging-*` are removed. The previous generation survives one swap because the render paths (today serialised behind the kernel gate, after `operator-render-switch` concurrent) may still be reading it; `operator-render-switch` owns tightening this if a ref-count turns out to be needed.
- Boot: the manager removes everything under `<platform-dir>` before starting controllers. Regeneration is the initial reconcile: the store starts empty, the informer's Create event passes `GenerationChangedPredicate` (it filters Update events only), and every render path gates on the store, so nothing renders before the module exists. No separate boot path exists.
- Deletion of the CR clears the store; directories are left to the next generation's prune or the next boot.

### The core pin comes from an operator constant (was OQ3)

**Context**: the CR names catalog versions but not a core version; the module must state one, and D13's promotion reads it.
**Options considered**: the library's schema pin (`schema.DefaultSchemaModule`, today `v2.0.0-alpha.6`, deliberately pre-D5 until the library's platform code follows the new shape; the schema cache resolves the same string); a manager flag; a compiled-in operator constant.
**Decision**: `platformmodule.CoreVersion`, a compiled-in constant (`v2.0.0-alpha.7` at this change), bumped by the workspace pin tooling like the other pins that live outside a `cue.mod` (`.tasks/deps/platform-pins.sh`). The constant is a root of the closure, so a catalog requiring a newer core raises the stated pin by maximum selection, exactly as `cue mod tidy` would; the file states what the build runs either way.
**Rationale**: the library's schema pin answers a different question (which core the kernel validates instances against) and cannot name the D5 core today; a flag invites two operators of one build to disagree. A constant is deterministic per operator build, which is the requirement. When the library's schema pin catches up with the platform shape, switching the source to a library-exported floor is a one-line change and is left to that moment.

### Module path is fixed (was OQ4)

**Decision**: `opmodel.dev/platforms/cluster@v0`, fixed. The singleton CR admits one platform; a fixed path keeps generated files byte-stable across generations (the same reason `renderstage.RenderModulePath` is fixed) and can never collide with an instance module's path (`renderstage.Promote` refuses equal paths). The namespace stays reserved-unpublished (D6).

### Status vocabulary and observability (was OQ5)

**Decision**:

- Ready=True, reason `Generated`: "Platform module generated and built for generation N".
- Ready=False, reason `GenerateFailed`: the operator could not write the module (filesystem I/O). This is the operator's own defect or a broken volume, never the CR's content.
- Ready=False, reason `BuildFailed`: the module's dependencies could not be resolved (closure derivation, naming the dependency path and version) or the module did not build (pin, key/import drift, schema conflict; the CUE error names the dependency or the `#registry` entry). Transient causes (network, timeout) requeue on the short interval exactly as materialize failures do today; other causes on the stalled recheck.
- `Materialized` and `MaterializeFailed` are retired from `internal/status`.
- Events: Normal `Generated` on success; Warning with the failure reason on a failure transition (unchanged gating on transition so periodic rechecks do not spam).
- No new metric (Principle VII); no status field for the module location. A pod-local path means nothing to a cluster reader; the reconcile log line carries the directory for support bundles.

### Package naming and store recording (was OQ6)

**Decision**: `internal/platformmodule` with three seams: `Generate` (CR spec plus closure to deterministic file bytes; pure, unit-tested for determinism, two-catalog, disabled-entry, stamp emission), `Closure` (registry-backed derivation, tested against a fake `ModFile` source), `Layout` (staging, swap, prune, reset; tested with a temp root). `internal/platform.Store` gains the generated-module record now (`SetGenerated` / `Generated`: generation, directory, `*platform.Platform`), and `Generation()` reports it. The materialized slot (`Set` / `Get`) keeps its readers untouched; nothing writes it any more, so the render paths report `PlatformNotReady` until `operator-render-switch` reads the new record. That gap is the proposal's stated release-train coupling.

### Generated content

```cue
// <platform-dir>/gen-3/cue.mod/module.cue  (modfile.Format canonical form)
module: "opmodel.dev/platforms/cluster@v0"
language: version: "v0.17.0"
deps: {
	"cue.dev/x/k8s.io@v0":         v: "v0.10.0"          // derived
	"opmodel.dev/catalogs/k8s@v1": v: "v1.0.0-alpha.2"   // CR pin
	"opmodel.dev/catalogs/opm@v4": v: "v4.0.1"           // CR pin
	"opmodel.dev/core@v2":         v: "v2.0.0-alpha.7"   // CoreVersion
}

// <platform-dir>/gen-3/platform.cue
// Generated by opm-operator from the cluster Platform (generation 3). Never edit, never publish.
package platform

import (
	core "opmodel.dev/core@v2"
	cat0 "opmodel.dev/catalogs/k8s@v1"
	cat1 "opmodel.dev/catalogs/opm@v4"
)

core.#Platform

metadata: name: "cluster"
type: "kubernetes"

#registry: {
	"opmodel.dev/catalogs/k8s@v1": {
		enable:   false
		version:  "1.0.0-alpha.2"
		#catalog: cat0
	}
	"opmodel.dev/catalogs/opm@v4": {
		enable:   true
		version:  "4.0.1"
		#catalog: cat1
	}
}
```

- Entries and imports are emitted in sorted key order; aliases are positional (`cat<N>`) so two catalogs sharing a last path element cannot collide.
- Imports are unqualified: CUE names the imported package after the import path's last element (`opm`, `k8s`), which is the convention both first-party catalogs follow. A catalog whose root package deviates fails the build naming the import; it surfaces as `BuildFailed`, not as a generation-time guess.
- `enable` is always written (the CR's pointer resolves to `true` when nil), so the file reads without knowing the schema default.
- The CR's object labels and annotations are Kubernetes metadata and are not carried into the module; nothing in the kernel reads platform labels, and the previous synth path's copy of them was incidental.
- `language.version` is `v0.17.0`, the floor `renderstage` and every published first-party module declare.

## Error handling

Generation failures (I/O) and build failures (closure, pin, drift, schema) both surface as Ready=False with wrapped causes naming the file, dependency or entry; reconcile requeues per the existing reconciler conventions (short interval for transient causes, stalled recheck otherwise). No error leaves a partially-written module recorded as current: the store record is written only after the swap and the build both succeed, and a failed build leaves the previous record (and its directory) in place.

## Migration Plan

Lands on the 0019 release train; branch-ordered after `library-platform-source`, released with `operator-render-switch`. Rollback pre-release is a revert; the CR is untouched, so no stored-object migration exists in any direction. The `--platform-dir` flag has a working default on the shipped manifest, so `dist/install.yaml` needs no change.
