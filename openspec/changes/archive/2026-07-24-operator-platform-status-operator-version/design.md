# Design: operator-platform-status-operator-version

## Context

Enhancement 0006/D24 requires the operator to self-publish its version to `Platform.status.operatorVersion`; the mechanism was left to this slice. The operator has no version identity today: nothing in `Makefile`, `Dockerfile`, or `.github/workflows/release.yml` tells the binary its version. The repo releases via release-please (`release-please-config.json`, `versioning: prerelease`, current manifest `1.0.0-alpha.2`); the `PlatformReconciler` writes status through a Flux `SerialPatcher` on both its success and failure paths.

## Goals / Non-Goals

**Goals:**

- A version identity that is a property of the source at the tag — correct in every build path (release image, local `go build`, kind e2e, `go install`) with no build-system cooperation.
- `status.operatorVersion` present on the singleton Platform whenever the operator has reconciled it, including when materialization fails.
- A stable value contract slice C1 can code against.

**Non-Goals:**

- No CLI-side ceiling check (C1's scope, 0006/D33).
- No ldflags/Dockerfile `ARG` plumbing — explicitly rejected (see decision 1).
- No stamping from `ModuleInstanceReconciler` — the Platform is the one guaranteed-fresh publish point (D24 chose it because its store is the render input the operator always reconciles).

## Decisions

### 1. Version burned into source via a release-please-maintained constant (user decision, 2026-07-09)

`internal/version/version.go` carries `const Version = "1.0.0-alpha.2" // x-release-please-version`; `release-please-config.json` adds `extra-files: ["internal/version/version.go"]` so the Release PR bumps the constant in the same commit the tag lands on. Every build of a tagged commit reports the right version — Docker (no `.git` in the build stage), local builds, `go install` — with zero changes to Dockerfile/Makefile/release.yml.

*Alternatives considered:*
- **ldflags `-X` via Dockerfile `ARG` + CI build-arg**: the version lives in the build invocation; every pipeline (Makefile, Dockerfile, CI, future goreleaser) must independently pass it and any that forgets silently yields "dev". Rejected as fragile.
- **Go ≥1.24 VCS stamping (`ReadBuildInfo().Main.Version`)**: automatic, but requires `.git` in the Docker build stage — absent from the kubebuilder Dockerfile, so the release image (the build that matters most) would report `(devel)`. Fixing it costs `COPY .git` cache churn + full tag fetch in CI. Rejected as fragile exactly where it matters; used only as best-effort *suffix* (decision 2).

### 2. Value contract: `v` + constant, best-effort VCS suffix on dev builds

`version.Full()` returns `"v" + Version` (matching release tags, e.g. `v1.0.0-alpha.2`); when `debug.ReadBuildInfo()` exposes `vcs.revision`, it appends `+g<short-sha>` (and `.dirty` when `vcs.modified`). Release images lack `.git` → clean tag; local/e2e builds get provenance. Known accepted weakness: a binary built from `main` between releases claims the previous release (under-report, dev clusters only). **Contract for C1:** the value is always `v`-prefixed semver, possibly carrying `+…` build metadata — C1's ceiling comparison MUST strip/tolerate the `+` suffix.

### 3. Stamp unconditionally on both reconcile exits

`plat.Status.OperatorVersion = version.Full()` is assigned beside `plat.Status.ObservedGeneration` on the success path and inside `failMaterialize`. The field means "the operator version running against this Platform" — C1 needs it precisely when things are mid-flight, so it must not be gated on `Ready`. No `patchStatus` change: the serial patcher patches the whole status object; `WithOwnedConditions` scopes only conditions.

### 4. Small observability riders

`kubectl get platform` shows the version via a printcolumn (`Operator` ← `.status.operatorVersion`), and `cmd/main.go` logs `version.Full()` at startup right after `ctrl.SetLogger` — the human-facing versions of the same fact.

## Risks / Trade-offs

- **[Generic-updater miss]** The `x-release-please-version` annotation bump is only provable on the next Release PR. → Verify on that PR; fallback: switch the extra-files entry to the JSON-typed form or a dedicated version file. A unit test asserts the annotation stays on the const line so a reformat can't silently detach it.
- **[Dev under-report]** `main`-built binaries claim the last release. → Accepted (dev clusters only); the VCS suffix marks such builds when `.git` is present.

## Migration Plan

Additive status field; no migration. Rollback = revert; the field disappears from newly-patched status on the next reconcile cycle (stale value on an un-reconciled Platform is harmless — D24 treats it as a ceiling, and downgrade-rollback sequencing is already covered by 0006's operational doc).

## Open Questions

None.
