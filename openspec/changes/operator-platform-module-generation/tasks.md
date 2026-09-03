# Tasks: operator-platform-module-generation

Task 1 is the gate for everything below it: this change was authored intent-first, and the second design pass must land its answers into `design.md` (and any affected spec scenarios) before implementation starts.

## Design (second pass)

- [x] 1. Resolve `design.md`'s six open questions in a second pass (transitive-closure/tidy with a measurement shared with `cli-config-platform-module`, disk layout + boot regeneration, core-pin source, module path, status vocabulary + observability, package naming + store recording); update design.md and the delta spec where answers add or sharpen scenarios.

## Generation (after task 1)

- [x] 2. Platform-module generation package: CR spec → deterministic module content (`cue.mod/module.cue` with pins per the resolved closure answer, `platform.cue` with importing entries + expected-version stamps, disabled entries preserved); unit tests for determinism, two-catalog, disabled-entry, and stamp emission.
- [x] 3. Disk lifecycle per the resolved layout: write-then-swap (no partially-written module ever current), superseded-generation cleanup, boot regeneration path; unit tests with a temp root.

## Reconciler

- [x] 4. `PlatformReconciler`: replace synthesize+materialize with generate+build (kernel shape-gated platform loader, operator registry mapping); Ready condition and reason vocabulary per the resolved status answer; keep generation recording compatible with the store until `operator-render-switch`.
- [x] 5. Integration tests (envtest): Ready=True on a clean CR, Ready=False naming the path/version for a nonexistent pin, Ready=False naming the entry for a generation-defect conflict; determinism across repeated reconciles of one generation.

## Docs and verification

- [x] 6. Update `api/v1alpha1/platform_types.go` and `internal/platform/store.go` doc comments (retire "maps onto synth.PlatformInput" prose; name the generated-module record); `docs/` design note if the second pass produced one.
- [x] 7. `task dev:fmt dev:vet dev:test` green; `task dev:lint` for the new package; note e2e deferral to `operator-render-switch` (the module is not consumed until then).

## Verification notes

- `task dev:fmt dev:vet dev:test` and `task dev:lint` green on 2026-09-03 against GHCR (`CUE_REGISTRY` mapping from the root policy): the registry-backed Platform specs (clean two-catalog CR, byte-identical regeneration, nonexistent pin, generation defect, build recovery) ran rather than skipped.
- e2e is deferred to `operator-render-switch`: nothing consumes the generated module until the render paths read the store's generated record, and until then every ModuleInstance and ModulePackage reports `PlatformNotReady`, so the Kind suite cannot pass on this change alone. The two changes land in one release train (proposal § Sequencing).
- The library pin stays at `v1.0.0-alpha.23`: the build goes through `Kernel.LoadPlatformPackage`; `AcquirePlatformFromDir` (alpha.24, release pending) is adopted by `operator-render-switch`.
