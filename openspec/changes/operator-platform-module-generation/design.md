# Design: operator-platform-module-generation

## Context

See `proposal.md` § Why. This design is deliberately first-pass: it fixes only what 0019 and the owner have already decided, and records everything else as open questions for a second pass on this change before implementation. Current state: `PlatformReconciler` decodes the CR spec into `synth.PlatformInput`, calls `SynthesizePlatform` + `Materialize`, and stores the `*MaterializedPlatform` in the single-slot `internal/platform` store behind the kernel gate (the 0019 stopgap).

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
**Rationale**: the render build consumes directory replacements served from disk; a persistent per-generation module makes regeneration observable, diffable in support bundles, and shared across the render paths without re-deriving. Disk is ephemeral pod state and the CR is the source of truth, so loss is repaired by regeneration (see OQ2).

### Generation replaces synthesis; the CR shape does not move

**Context**: `SynthesizePlatform` is deleted by the library wave; the CR is a stable API.
**Decision**: the reconciler maps `spec.registry` to the module template (the same shape `cli-config-platform-module` establishes for the CLI seed) with the CR's `version` values used twice: as the `cue.mod` catalog pins and as the stamped expected `version` on each entry (the D13 tripwire).
**Rationale**: D6 verbatim. The double use is not two answers: the stamp unifies with the derived readout, so its only outcomes are agreement or a build conflict naming the entry.

### Validation is a real build through the kernel loader

**Context**: a generated module can be wrong three ways: bad pin, key/import drift, generation defect.
**Decision**: after writing, build via the kernel's shape-gated platform loader (the source-carrying acquire once `library-platform-source` lands) against the operator's `--registry` mapping; Ready reflects the outcome.
**Rationale**: the build is the only check that proves the pins resolve and exercises the schema's own tripwires; anything less re-implements them.

## Open Questions (the second pass resolves these before implementation)

1. **Transitive closure / generation-time tidy.** D13 says tidying happens once, at platform-package generation, and 0019 measured that a default-major dependency (`cue.dev/x/k8s.io`) is honored only as a root entry. Does the generated `cue.mod` need the full tidied closure (catalog transitive deps included), and if so, does generation run a tidy-equivalent (registry I/O on the reconcile path) or derive the closure from the pulled catalogs' own module files? Whatever the answer, it applies equally to the CLI's hand-written seed (`cli-config-platform-module`) and should be settled once, with a measurement.
2. **Disk layout and lifecycle.** Directory scheme (per-generation dir vs single dir rewritten), the volume (writable container FS vs emptyDir vs explicit mount), cleanup of superseded generations, and regeneration on operator start (disk is ephemeral; the CR is the source of truth, so boot MUST be able to rebuild the module before the first render).
3. **The core pin's source.** The CR names catalog versions but not a core version. Candidates: the operator's embedded library floor, the schema cache's resolved version, a manager flag. Must be deterministic per operator build and stated in the module for D13's promotion to read.
4. **Module path spelling.** A fixed `opmodel.dev/platforms/cluster@v0` versus a generation-derived name; fixed is presumed (the singleton CR admits one platform) pending the second pass.
5. **Status vocabulary and observability.** Ready reason set replacing Materialized/MaterializeFailed, whether generation emits an event and a metric, and what (if anything) of the module location appears in status.
6. **Package naming and store recording.** Where generation lives (`internal/platformmodule` presumed) and whether the module path + generation record lands in the existing store now or only in `operator-render-switch`.

## Error handling

Generation failures (I/O) and build failures (pin, drift, schema) both surface as Ready=False with wrapped causes naming the file or entry; reconcile returns the error for backoff per the repo's existing reconciler conventions. No error may leave a partially-written module recorded as current (write-then-swap or equivalent; second-pass detail under OQ2).

## Migration Plan

Lands on the 0019 release train after the second design pass; branch-ordered after `library-platform-source`, released with `operator-render-switch`. Rollback pre-release is a revert; the CR is untouched, so no stored-object migration exists in any direction.
