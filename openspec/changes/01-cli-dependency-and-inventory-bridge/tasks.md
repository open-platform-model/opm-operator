## 1. Copy CLI packages to `pkg/`

Copy CLI packages from `cli/pkg/` to the controller's `pkg/` directory. Update all internal import paths from `github.com/opmodel/cli/pkg/` to `github.com/open-platform-model/poc-controller/pkg/`. Source: `/var/home/emil/Dev/open-platform-model/cli/pkg/`.

- [ ] 1.1 Copy `pkg/core` (convert.go, labels.go, resource.go)
- [ ] 1.2 Copy `pkg/errors` (config_error.go, domain.go, errors.go, sentinel.go)
- [ ] 1.3 Copy `pkg/validate` (config.go) — update import of `pkg/errors`
- [ ] 1.4 Copy `pkg/provider` (provider.go)
- [ ] 1.5 Copy `pkg/module` (module.go, parse.go, release.go) — update import of `pkg/validate`
- [ ] 1.6 Copy `pkg/bundle` (bundle.go, release.go) — update import of `pkg/module`
- [ ] 1.7 Copy `pkg/loader` (provider.go, release_file.go, release_kind.go) — update import of `pkg/provider`
- [ ] 1.8 Copy `pkg/render` (bundle_renderer.go, errors.go, execute.go, finalize.go, match.go, module_renderer.go) — update imports of `pkg/core`, `pkg/errors`, `pkg/module`, `pkg/provider`, `pkg/bundle`, `pkg/validate`. Do NOT copy `process_modulerelease.go` or `process_bundlerelease.go` here — they are relocated in 1.9.
- [ ] 1.9 Relocate process files to domain packages (design decision 6): copy `pkg/render/process_modulerelease.go` to `pkg/module/process.go` and rename `ProcessModuleRelease` to `Process`; copy `pkg/render/process_bundlerelease.go` to `pkg/bundle/process.go` and rename `ProcessBundleRelease` to `Process`. Update imports in both files and any internal callers.
- [ ] 1.10 Copy `pkg/resourceorder` (weights.go)
- [ ] 1.11 Copy relevant test files for copied packages
- [ ] 1.12 Run `go mod tidy` to add `cuelang.org/go` and other transitive dependencies
- [ ] 1.13 Run `go build ./pkg/...` to verify all copied packages compile

## 2. Inventory entry functions

- [ ] 2.1 Copy `IdentityEqual`, `K8sIdentityEqual`, and `NewEntryFromResource` from `cli/pkg/inventory/entry.go` into `internal/inventory/entry.go`, rewriting to operate on `v1alpha1.InventoryEntry`. Add `LabelComponentName` constant (from `cli/pkg/core/labels.go`).
- [ ] 2.2 Write unit tests for identity comparison (version excluded, component included/excluded) and entry construction from unstructured resource

## 3. Stale set computation

- [ ] 3.1 Copy `ComputeStaleSet` from `cli/pkg/inventory/entry.go` into `internal/inventory/stale.go`, rewriting to operate on `v1alpha1.InventoryEntry` (replaces empty stub)
- [ ] 3.2 Write unit tests for stale set computation (stale entries detected, no stale, version-agnostic identity)

## 4. Digest computation

- [ ] 4.1 Copy `ComputeDigest` from `cli/pkg/inventory/entry.go` into `internal/inventory/digest.go`, rewriting to operate on `v1alpha1.InventoryEntry` (replaces empty stub)
- [ ] 4.2 Write unit tests for digest computation (determinism across ordering, content sensitivity)

## 5. Validation

- [ ] 5.1 Run `make fmt vet lint test` and verify all checks pass
