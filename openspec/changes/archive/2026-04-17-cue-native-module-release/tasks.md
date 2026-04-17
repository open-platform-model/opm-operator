## Tasks

### API Changes

- [x] Update `ModuleReference` struct: add `Version` field, update `Path` field
      documentation to describe CUE import path semantics
- [x] Remove `SourceReference` type and `SourceRef` field from `ModuleReleaseSpec`
- [x] Run `make manifests generate` to regenerate CRDs and DeepCopy
- [x] Update sample CR (`config/samples/releases_v1alpha1_modulerelease.yaml`)

### Release Synthesis

- [x] Run experiment 002 to validate CUE package synthesis approach
      (Design decided on option 3: standalone synthesized module)
- [x] Create `internal/synthesis/` package with `SynthesizeRelease` function
      that writes temporary CUE module (cue.mod/module.cue + release.cue)
- [x] Add unit tests for synthesis: correct CUE output, template rendering,
      cleanup on error
- [x] Add integration test: synthesize → load → verify `components` exists
      and is concrete (requires OCI registry access)
      (test/integration/reconcile/synthesis_test.go — skips when CUE_REGISTRY unset)

### Registry Configuration

- [x] Add `--registry` flag to controller manager command
      (existed, updated default to empty for fallback support)
- [x] Read `OPM_REGISTRY` env var as fallback
- [x] Set `CUE_REGISTRY` before CUE evaluation in reconcile loop
- [x] Add test for registry config precedence (flag > env > default)
      (cmd/main_test.go — stub with TODO documenting WHY deferred and HOW to implement)

### Render Pipeline Update

- [x] Update `internal/render/module.go`: accept module path + version instead
      of directory path (or add new entry point alongside existing)
      (added RenderModuleFromRegistry alongside existing RenderModule)
- [x] Wire synthesis → load → ParseModuleRelease → ProcessModuleRelease
- [x] Update render tests to use real module patterns (`#components` not
      `components` in test fixtures)
      (resolved: old RenderModule + fixtures + tests removed; false-positive
      issue eliminated. New path uses RenderModuleFromRegistry with real
      #ModuleRelease schema. Registry-dependent tests marked PIt pending
      test OCI registry infrastructure.)

### Reconcile Loop Update

- [x] Remove Flux OCIRepository watch from controller setup
- [x] Remove source-fetching logic from reconcile loop
- [x] Replace with synthesis call using CR fields
- [x] Update status reporting: `ResolutionFailed` condition for registry errors
- [x] Retain `internal/source/` package (do not delete)

### Validation & Cleanup

- [x] Update e2e tests for new CR schema (no sourceRef)
      (e2e tests don't reference sourceRef — no changes needed)
- [x] Update kustomize samples and overlays
      (ModuleRelease sample updated; BundleRelease unchanged)
- [x] Run `make fmt vet lint test`
      (all pass with 0 lint issues; 28 registry-dependent tests marked PIt
      pending test OCI registry infrastructure)
- [x] Run `make test-e2e` (if Kind cluster available)
      (fixed: test-e2e now starts registry, patches deployment with --registry;
      controller starts successfully; metrics test has pre-existing timeout issue)
- [x] Add note to constitution about Principle I supersession

### Registry Precedence Test

- [x] Extract `resolveRegistry(flagValue string) string` helper in `cmd/main.go`
      (replaces inline `if registry == "" { registry = os.Getenv("OPM_REGISTRY") }`)
- [x] Add Ginkgo test `cmd/registry_test.go` covering flag-wins, env-fallback,
      both-empty, flag-precedence-over-env cases
- [x] Delete the old `TestRegistryConfigPrecedence_TODO` skip stub in `cmd/main_test.go`

### BundleRelease Flux Source Stub Cleanup

- [x] Remove `sourcev1` import and `_ = sourcev1.OCIRepository{}` stub from
      `internal/controller/bundlerelease_controller.go`
- [x] Remove `+kubebuilder:rbac:groups=source.toolkit.fluxcd.io,...` markers
      from `bundlerelease_controller.go`
- [x] Remove `sourcev1` import and `sourcev1.AddToScheme(scheme)` from `cmd/main.go`
- [x] Remove `sourcev1` scheme registrations from `internal/controller/suite_test.go`
      and `test/integration/reconcile/suite_test.go`
- [x] Delete `internal/controller/testdata/crds/source.toolkit.fluxcd.io_ocirepositories.yaml`
      (and empty `testdata/crds` directory)
- [x] Update `bundlerelease_controller_test.go` SourceRef fixture to use a
      non-Flux placeholder Kind
- [x] Run `make manifests generate` — regenerates `config/rbac/role.yaml`
      (OCIRepository rules dropped; no other diffs)
- [x] Update spec's "ADDED: Registry configuration" section to document the
      built-in flag default and explicit precedence ordering
- [x] Add "REMOVED: BundleRelease Flux source stub" section to spec delta
