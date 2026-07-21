# Design: live-flux-e2e

## Context

The e2e suite (`test/e2e`, `task dev:e2e`, kind-backed) deploys the controller and runs `podinfo_test.go` for real in CI (registry credentials publish the example modules there). `test/fixtures/modulepackages/podinfo/` already contains the exact fixture a live-Flux spec needs — `modulepackage.yaml` plus an `ocirepository.yaml` pointing at `oci://opm-registry:5000/...` — with no live consumer. The `.tasks/flux.yaml` install task exists but installs source-controller unversioned, which would silently test whatever Flux is newest rather than the distribution set the operator's libraries pin (A1/D4: flux2 v2.9.0). A5 (0006) proved the full loop manually via opm-kind-demo; that repo is a demo, not a test vehicle, so the coverage must live here.

## Goals / Non-Goals

**Goals:** real-artifact pipeline proof; deployed-controller lifecycle/prune/orphan proof; version pin tying the installed source-controller to the Go library line; CI wiring; stub debt retired rather than left as permanent Skips.

**Non-Goals:** kustomize-controller (the operator never consumes it); multi-node/perf scenarios; `concurrent_test.go`'s parallel-instances stub (stays recorded); fixing any behavior bug a live spec surfaces (pause and report — this change is test-infra only).

## Decisions

### LD1: Source-controller only, pinned beside the library versions

`flux install --components=source-controller --version=<pin>` where the pin is a single variable in `.tasks/flux.yaml` documented as "must match the flux2 distribution whose library versions `go.mod` pins (A1/D4)". Rationale: the compat claim under test is *this operator against the source-controller its libraries were built for* — an unpinned install tests a moving target; installing kustomize-controller would add reconcilers that fight the test's own applies for no coverage gain.

### LD2: The artifact pipeline spec drives the existing fixture end-to-end

Sequence: ensure local registry (existing `.tasks/registry.yaml` machinery) → `flux push artifact oci://.../podinfo-package:<tag>` packing `test/fixtures/modulepackages/podinfo/` content → apply `ocirepository.yaml` + `modulepackage.yaml` → `Eventually`: OCIRepository `Artifact` present (real revision/digest), ModulePackage Ready, rendered Deployment Ready; assert the artifact revision string propagates into ModulePackage status (locks the fetch/extract/validate path over a genuine tarball, `internal/source/`). Failure modes this uniquely catches: source-controller API drift (`source-controller/api` v1.9.x types), artifact URL/serving changes, tarball layout assumptions.

### LD3: Lifecycle spec extends the proven podinfo pattern rather than a new fixture

`podinfo_test.go` already stands up Platform + ModuleInstance live. The new spec reuses that flow: create→Ready (existing) → mutate values so the render drops a resource → assert live prune by the deployed controller → recreate → `prune=false` + delete → CR gone, workloads remain (orphan), namespace intact; finalizer presence asserted across add/remove. This is the deployed-controller half the envtest tier structurally cannot provide (real SSA against a real API server through the running manager, real requeue timing).

### LD4: Stub disposition is explicit, per stub

- `lifecycle_test.go` 5.1/5.2 → replaced by LD2+LD3 specs (same file, stubs deleted).
- `prune_test.go` five stubs → logic covered at envtest (`test/integration/apply/prune_test.go` + `stale_pruning` fills from `envtest-coverage-batch`); live-tier value subsumed by LD3's prune assertion → file deleted with a pointer comment in the lifecycle spec.
- `finalizer_test.go` four stubs → same disposition via LD3's finalizer/orphan assertions → deleted.
- `concurrent_test.go` → kept: parallel-instances stub stands as the recorded future item; controller-pod-kill idempotency is an optional stretch task here (delete pod mid-reconcile, assert convergence) — cheap if the suite proves stable, dropped without guilt otherwise.

### LD5: CI gains flux CLI, pinned, and hard-fails

`test-e2e.yml` installs the flux CLI at the same pinned version (official install script with version arg or `fluxcd/flux2` action) and runs the source-controller install step before the suite. The new specs hard-fail (not skip) when source-controller is absent in CI (`OPM_E2E_FLUX=1` set there), and skip-with-notice locally when the operator's e2e env lacks it — matching the suite's existing gating idiom.

## Risks / Trade-offs

- [Flux CLI/toolkit version skew in CI (the A5 lesson: CLI refuses to install a newer toolkit)] → both pins come from the same variable; the install step echoes both versions.
- [Registry-in-kind networking flakiness] → reuse the exact container/network wiring the existing registry tasks + CI already use for module publishing; nothing novel.
- [Live prune assertion races the reconcile interval] → bounded `Eventually` with the suite's standard timeouts; assert on inventory/status first (deterministic), live-object absence second.
- [Suite runtime grows] → source-controller install ~seconds; one artifact push; acceptable for the tier that has never existed.
