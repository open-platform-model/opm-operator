# Design: operator-platformmodule-lift

## Context

See `proposal.md` § Why. `internal/platformmodule` has three seams (`Generate`, `Closure`, `Layout`) and constants (`ModulePath`, `CorePath`, `CoreVersion`, `LanguageVersion`, file names). The library helper (`library-platform-module-generator`) provides the first two seams, the file-name constants, `Roots` with a core-pin default from `schema.DefaultSchemaModule`, `RegistryConfig{Registry, ClientType, Env}` for the module-file source, and `Files.WriteTo`. Its generator is proven byte-identical to this repo's for the same input. The reconcile sequence (`platform_controller.go`: entries → closure → generate → `Layout.Write` under the kernel gate → `AcquirePlatformFromDir` → store → prune) and the store's lease model are unchanged by `operator-render-switch` and unaffected here.

## Goals / Non-Goals

**Goals**

- Zero behavioural change for the CR, the conditions, the events and the on-disk lifecycle; the same bytes on disk for the same CR and core pin.
- One owner for the core pin: the library.

**Non-Goals**

- Lifting `Layout`. It encodes CR generations, the render-gate ordering and the ephemeral-volume boot reset; the CLI has a different lifecycle. The library helper deliberately offers only `WriteTo`.
- Any change to `operator-render-switch`'s render path or the store.

## Decisions

### `Layout` moves to `internal/platform`, `internal/platformmodule` is deleted

**Options**: (1) keep `internal/platformmodule` holding only `layout.go`; (2) move `Layout` beside the store in `internal/platform`.
**Decision**: option 2. A package whose only content is the lifecycle of directories the store records belongs with the store; a leftover `platformmodule` package next to the library's `platformmodule` import invites confusion. The reconciler's `Layout.Write` returns the same path, and `Store.Generated.Dir` is unchanged.

### The core pin comes from the library, not a constant

**Context**: `CoreVersion` was the D6 design's "operator constant" answer (OQ3), bumped by `.tasks/deps/platform-pins.sh`.
**Decision**: call the helper's `Roots(entries)` with no override; the pin is the library's `DefaultSchemaModule` version. The sample Platform and e2e fixtures do not name a core version, so nothing else moves. The pin-script stanza is deleted in the workspace root as follow-through; the archived D6 design decision is superseded by this one and recorded here, the archive is not edited.
**Rationale**: the only correct pin for a generated platform is the release the library verified its glue against; a separately bumped constant can only match it or be wrong.

### Explicit registry config

**Decision**: `platformmodule.NewRegistry(platformmodule.RegistryConfig{Registry: r.Registry, ClientType: "opm-operator", Env: os.Environ()})` in the reconciler's `modFiles`, replacing the in-tree `NewRegistry(registry)`. `os.Environ()` at the operator edge is the explicit pass-through the library requires (the process already sets `CUE_CACHE_DIR` at manager start).

### Proof of equivalence

**Decision**: before deleting the in-tree generator, one test in this change's PR generates the sample Platform's input through both packages and asserts byte equality of both files; it is removed with the in-tree package in the same PR (the library's golden tests keep the contract). The e2e suite (`test/e2e`, GHCR-backed) runs unchanged as the behavioural proof.

## Risks / Trade-offs

- [A library bump moves the core pin under a running cluster without a Platform edit] → intended and already the case for the render glue; the generated module is regenerated at manager start, and the skew policy reports any module/platform disagreement.
- [The library release with the helper is not yet cut when this change starts] → the change is gated on it; the `go.mod` bump is task 1.

## Migration Plan

Single PR: bump, re-point, move `Layout`, delete, tests. No CRD, manifest or `dist/install.yaml` change. Rollback is a revert. No migration note for users: the CR API and status are unchanged.

## Open Questions

None.
