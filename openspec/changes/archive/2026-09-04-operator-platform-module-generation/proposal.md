## Why

Enhancement 0019 D6: the Platform CR keeps naming catalog coordinates in typed fields, and the operator generates the `#Platform` CUE package those coordinates describe. Under D5 a platform's registry entry carries its catalog by import, resolved by the platform module's own `cue.mod`, so the CR's spec can no longer be handed to the kernel as typed data: `SynthesizePlatform` is deleted by the library's 0019 wave, and the render build consumes a platform module on disk. The operator therefore writes that module to its own filesystem and validates it, keeping the CR's API surface unchanged.

This change is `operator-platform-module-generation`, the operator's D6 slice of the 0019 Phase B wave. Deliberately intent-level: the mechanism details (disk layout, lifecycle, core-pin source, tidy behavior) are recorded as open questions and ironed out in a second pass on this change before implementation, not decided here.

## What Changes

- The `PlatformReconciler` stops calling `SynthesizePlatform` + `Materialize` and instead, per reconciled generation of the singleton Platform CR, **generates a platform CUE module on the operator's own disk**: a `cue.mod/module.cue` (reserved-unpublished module path under `opmodel.dev/platforms/…`, catalog pins taken verbatim from the CR's `spec.registry` versions, plus the core pin) and a `platform.cue` (embeds `core.#Platform`; one `#registry` entry per subscription, carrying the catalog by import and stamping the CR's expected `version` so wrong bytes become a build conflict naming the entry, the D13 tripwire).
- The reconciler validates the generated module by building it through the kernel's shape-gated platform loader; the Ready condition reflects the build (Ready=True on a clean build, Ready=False with the naming error on a bad pin, key/import drift, or unresolvable dependency). Status vocabulary moves from "materialized" to "generated and built"; exact reasons are second-pass detail.
- The generated module's location (plus the generation it was built for) becomes the process-local record the render path will consume; the existing store keeps working until the operator's render-switch change consumes the module and retires the held `*MaterializedPlatform` slot (0019 D8).
- **No API change**: `PlatformSpec`/`Subscription` keep their shape (`registry[path].version` stays required; D6 keeps the CR naming coordinates). No CRD regeneration expected.

Out of scope, as siblings in the same wave: switching the render path to `Kernel.Render` against the generated module and retiring the store's held platform + kernel gate (`operator-render-switch`); any skew-policy surface on the operator (0019 D7/D18, likely a manager flag or CR field, decided there); publishing a platform anywhere (disallowed outright, D6).

## Sequencing

Gates on the D5/D17 core prerelease (`core-registry-import`) and on the library wave (`library-platform-source` for the source-carrying acquire; the loader path itself exists today). Between this change and `operator-render-switch` the operator's render path still runs the old pipeline, which cannot consume the generated module, so the two land in one release train; this change first only in branch order.

## SemVer classification

Operator releases by image under conventional commits; behaviorally this is internal (`feat` scope), with no API or CRD change. The user-visible contract change (what Ready means for a Platform) rides the wave's release notes.

## Affected API types and controllers

- API: none (shape unchanged; doc comments on `PlatformSpec`/`Subscription` update their "maps onto synth.PlatformInput" prose).
- Controllers: `PlatformReconciler` (generation + build replaces synthesize + materialize).
- Internal: a new platform-module generation package; `internal/platform` store touched only in doc comments here (reshaped in `operator-render-switch`).
- Fixtures/samples: `config/samples` Platform and e2e platform fixtures keep their CR shape; expected-version stamps make their pins load-bearing exactly as today.

## Complexity justification

The operator already owns the two halves this composes: typed CR decode and kernel calls. Generation replaces the deleted `SynthesizePlatform` with file emission the CLI change (`cli-config-platform-module`) already establishes the template shape for; the operator variant adds only the CR-driven parameterization and the expected-version stamp.

## Capabilities

### New Capabilities

- `platform-module-generation`: the operator-generated platform module: what the reconciler writes, what the CR fields map to, the expected-version tripwire, build validation and its Ready reflection, and the never-published guarantee.

### Modified Capabilities

None in this change. `platform-reconciler`'s materialize-era requirements and `platform-gated-rendering`'s store contract are reworked by `operator-render-switch`, which owns those deltas.

## Impact

- `internal/controller` (PlatformReconciler), new `internal/platformmodule` (or similar; second-pass naming), `internal/platform/store.go` doc comments, `api/v1alpha1/platform_types.go` doc comments only.
- `enhancement.yaml` declares 0019 D6 (with the D13 stamp clause).
