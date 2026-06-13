## Context

The operator imports no `open-platform-model/library` code today; it runs a
pre-0001 fork of the OPM runtime (`pkg/render`, `pkg/module`, `pkg/loader`,
`pkg/provider`, `pkg/validate`, `internal/{catalog,synthesis,render}`) built
directly on `cuelang.org/go`. Enhancement 0001's operator rewrite replaces that
fork with calls into the library kernel. `KERNEL-MIGRATION-NOTES.md` lays out the
full migration; §5.1 is this step.

The library's 0001 work (CUE v0.17 recontract, OCI schema loader, `Materialize`,
concurrent render, `SynthesizePlatform`) shipped as **`library v0.3.0`** —
confirmed present on origin and carrying `SynthesizePlatform`, pinned to
`cuelang.org/go v0.17.0-alpha.1`, the exact CUE version the operator already
uses. This change consumes that tag and proves the integration before any render
path is touched.

Kernel lifetime contract (from `library/CLAUDE.md`): long-running consumers MUST
keep one Kernel alive — one `*schema.Cache` per Kernel, the core schema fetched
once on first use. The manager process is exactly such a consumer.

## Goals / Non-Goals

**Goals:**

- Add `github.com/open-platform-model/library v0.3.0` and confirm the operator
  compiles and tests pass with it on the shared `v0.17.0-alpha.1` toolchain.
- Construct one long-lived `*kernel.Kernel` in `cmd/main.go`, configured from the
  existing `--registry`/`OPM_REGISTRY` input and the controller logger.
- Verify core-schema resolution at startup (fail-fast), logging the resolved
  version.
- Inject the Kernel into reconcilers as the seam later slices consume.

**Non-Goals:**

- Rewiring any render path. `internal/render`, `pkg/render`, `internal/synthesis`,
  `internal/catalog`, `pkg/{module,loader,provider,validate}` are untouched and
  still drive every reconcile.
- Introducing the `Platform` CRD, `SynthesizePlatform`, or `Materialize` calls
  (subsequent slices).
- Removing the legacy provider load (`catalog.LoadProvider`) or its
  `--catalog-path`/`--provider-name` flags — they coexist this slice.
- Any CRD/API change.

## Decisions

### Consume the published `v0.3.0` tag, not a `replace` or branch pseudo-version

**Decision:** Add a normal `require github.com/open-platform-model/library v0.3.0`.
No `replace => ../library`, no branch pseudo-version.

**Rationale:** The workspace deliberately uses no `go.work` and no `replace`
directives; sibling repos consume each other as published modules. `v0.3.0` is on
origin and reproducible in CI. A local `replace` breaks CI (no `../library` in
the CI checkout) and a branch pseudo-version is non-reproducible while the branch
moves.

**Alternatives considered:** `replace => ../library` (fast local iteration, but
non-reproducible and against workspace convention); branch pseudo-version
(fetchable but moves). Both rejected as dev-only stopgaps.

### Source registry config from the existing flag, add no new surface

**Decision:** Feed `kernel.WithRegistry` from the value already resolved for
`--registry` / `OPM_REGISTRY` in `cmd/main.go`.

**Rationale:** The operator already accepts and plumbs this value to its legacy
CUE loader. Reusing it keeps the kernel and the legacy path pointed at the same
registry during the transition and avoids a speculative new flag (Principle VII).
`library/CLAUDE.md` requires frontends to set the registry mapping explicitly —
the operator already has the value.

**Alternatives considered:** a dedicated `--kernel-registry` flag — rejected as
unjustified surface; the two paths should resolve identically during migration.

### Smoke-verify schema resolution at startup, not lazily

**Decision:** Trigger one core-schema resolution during startup (e.g.
`k.SchemaCache().Get(...)` / read `ResolvedVersion()`), log the version, and fail
startup on error.

**Rationale:** Schema fetch is otherwise lazy (first `Validate`/`Match`/`Compile`).
With no render path wired this slice, nothing would exercise the Kernel — the
whole point of a de-risking slice is to prove the dependency *works*, so we force
the one observable behavior the Kernel has: it can reach the schema. Fail-fast
turns a misconfigured registry into a startup error, not a deferred reconcile
surprise.

**Alternatives considered:** lazy/no startup check — rejected; it would leave this
slice with nothing verifiable and defer integration failure.

### Inject as a struct field now, consume later

**Decision:** Add `Kernel *kernel.Kernel` to the render-bearing reconciler structs
and set it in `cmd/main.go`; do not read it on any render path yet.

**Rationale:** Establishes the exact seam §5.2 swaps behind, in a separately
reviewable commit, without entangling the dependency wiring with render-logic
changes. Go does not flag unused struct fields, so this is clean. It is not
speculative abstraction — the next slice in the same enhancement reads it.

**Alternatives considered:** construct the Kernel only in `main` and thread it in
during the render-swap slice — rejected; setting the seam now keeps the
render-swap diff focused on logic, not plumbing.

## Risks / Trade-offs

- **Transitive dependency conflict** (library pulls `cuelang.org/go` +
  `Masterminds/semver/v3` + otel) → both modules already pin
  `cuelang.org/go v0.17.0-alpha.1`; `go mod tidy` + `task dev:test` will surface
  any incompatibility immediately, which is the explicit purpose of this slice.
- **An injected-but-unused field reads as dead code** → mitigated by the design
  note and the fact that it is consumed within enhancement 0001's next slice; the
  spec records the intent.
- **Two registry consumers (kernel + legacy loader) drift** → mitigated by
  sourcing both from the single `--registry`/`OPM_REGISTRY` value; they cannot
  diverge by construction this slice.
- **Startup schema fetch adds a registry round-trip to boot** → acceptable and
  desirable (fail-fast); it is the same fetch the first reconcile would do, paid
  once, up front.

## Migration Plan

1. `go get github.com/open-platform-model/library@v0.3.0` + `go mod tidy`.
2. Construct the Kernel in `cmd/main.go`; add startup smoke check + version log.
3. Add the `Kernel` field to reconciler structs; set it at registration.
4. Run the validation gates (`task dev:manifests dev:generate` — expect no diff
   since no API change; `task dev:fmt dev:vet dev:lint dev:test`).

**Rollback:** revert the commit — the legacy render path is untouched, so removing
the require + construction returns the operator to its current behavior exactly.

## Open Questions

- Logger bridge shape: does the library accept the controller's `slog.Logger`
  directly via `kernel.WithLogger`, or is a thin adapter needed? Resolve when
  wiring (the `WithLogger` signature takes `*slog.Logger`; controller-runtime can
  surface an `slog` handler — confirm the cleanest bridge during implementation).
