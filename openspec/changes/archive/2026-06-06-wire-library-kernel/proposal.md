## Why

The operator carries a complete pre-0001 fork of the OPM runtime and imports **zero** library code today (`grep open-platform-model/library` returns nothing). Enhancement 0001's operator rewrite consumes the `open-platform-model/library` kernel instead — but before any render path is rewired, the dependency itself must be proven to integrate: the module must resolve, compile under the operator's CUE `v0.17.0-alpha.1` toolchain, and the kernel must successfully reach the OPM core schema. This change is the smallest possible first slice — it wires the dependency and constructs the kernel, nothing more — so every later slice builds on a known-good foundation rather than discovering integration breakage mid-rewrite.

## What Changes

- Add `github.com/open-platform-model/library v0.3.0` to `go.mod` (the first published tag carrying the v0.17 recontract, OCI schema loader, `Materialize`, concurrent render, and `SynthesizePlatform`; CUE pin matches the operator's `v0.17.0-alpha.1`).
- Construct **one long-lived `*kernel.Kernel`** in `cmd/main.go`, kept alive for the manager's lifetime (per `library/CLAUDE.md` — long-running consumers MUST keep one Kernel alive; one `*schema.Cache` per Kernel). Built with:
  - `kernel.WithRegistry(...)` sourced from the existing `--registry` flag / `OPM_REGISTRY` env (same value already plumbed to the legacy CUE loader).
  - `kernel.WithLogger(...)` bridged to the controller's `slog` logger.
  - the default schema loader (OCI-backed, resolving `opmodel.dev/core@v0`).
- Pass the constructed `*kernel.Kernel` into the reconcilers as a new struct field, **unused on the render path for now** — establishing the injection seam later slices consume.
- Smoke-verify core-schema resolution at startup (a one-shot `SchemaCache().Get` / `ResolvedVersion()` log line) so a misconfigured registry fails fast and visibly rather than silently at first reconcile.

No render path is rewired; the fork (`pkg/render`, `internal/{catalog,synthesis,render}`, …) is untouched and still drives all reconciliation. No CRD/API change. No `replace` directive (consume the published tag — reproducible).

## Capabilities

### New Capabilities

- `library-kernel-runtime`: the operator constructs and owns a single long-lived library `*kernel.Kernel` for its process lifetime, configured from existing registry/logger inputs, with core-schema resolution verified at startup and the kernel injected into reconcilers as the seam for subsequent render-path slices.

### Modified Capabilities

None — no existing capability's requirements change. The legacy render path (`module-release-synthesis`, `cue-rendering`, `catalog-provider-loading`, `module-renderer-interface`, …) is unchanged this slice and is retired in later 0001 slices.

## Impact

- **Dependencies**: new `github.com/open-platform-model/library v0.3.0` require in `go.mod` (+ transitive). First Go consumer of the library module in the workspace.
- **Code**: `cmd/main.go` (kernel construction + startup smoke check + injection); reconciler structs in `internal/controller/` gain a `Kernel *kernel.Kernel` field; `go.mod` / `go.sum`.
- **APIs/CRDs**: none.
- **Enhancement**: first operator-side implementation slice of workspace enhancement `0001`. `KERNEL-MIGRATION-NOTES.md` §5.1 describes this step; the notes' "no blocker remains" claim is corrected by this change's existence (the blocker was a consumable release — now `v0.3.0`).
- **SemVer**: MINOR — additive dependency + wiring; no behavior change to existing reconciliation, no API change.
- **Complexity justification (Principle VII)**: the only added surface is one kernel construction and an injected-but-unused field. The unused field is deliberate seam-setting for the next slice, not speculative abstraction — it is consumed within the same enhancement.
