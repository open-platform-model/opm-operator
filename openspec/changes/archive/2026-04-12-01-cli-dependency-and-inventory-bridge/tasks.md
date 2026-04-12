## 1. Copy CLI packages to `pkg/`

Copy CLI packages from `cli/pkg/` to the controller's `pkg/` directory. Update all internal import paths from `github.com/opmodel/cli/pkg/` to `github.com/open-platform-model/poc-controller/pkg/`. Source: `/var/home/emil/Dev/open-platform-model/cli/pkg/`.

- [x] 1.1 Copy `pkg/core` (convert.go, labels.go, resource.go)
- [x] 1.2 Copy `pkg/errors` (config_error.go, domain.go, errors.go, sentinel.go)
- [x] 1.3 Copy `pkg/validate` (config.go) — update import of `pkg/errors`
- [x] 1.4 Copy `pkg/provider` (provider.go)
- [x] 1.5 Copy `pkg/module` (module.go, parse.go, release.go) — update import of `pkg/validate`
- [x] 1.6 ~~Copy `pkg/bundle`~~ — excluded, bundle not yet implemented in OPM
- [x] 1.7 Copy `pkg/loader` (provider.go, release_file.go, release_kind.go) — update import of `pkg/provider`
- [x] 1.8 Copy `pkg/render` (errors.go, execute.go, finalize.go, match.go, module_renderer.go) — update imports of `pkg/core`, `pkg/errors`, `pkg/module`, `pkg/provider`, `pkg/validate`. Bundle renderer excluded.
- [x] 1.9 Copy `process_modulerelease.go` to `pkg/render/` (revised — relocation to `pkg/module` infeasible due to import cycle). `FinalizeValue` exported. Bundle process file excluded.
- [x] 1.10 Copy `pkg/resourceorder` (weights.go)
- [x] 1.11 Copy relevant test files for copied packages
- [x] 1.12 Run `go mod tidy` to add `cuelang.org/go` and other transitive dependencies
- [x] 1.13 Run `go build ./pkg/...` to verify all copied packages compile

## 2. Inventory entry functions

- [x] 2.1 Copy `IdentityEqual`, `K8sIdentityEqual`, and `NewEntryFromResource` from `cli/pkg/inventory/entry.go` into `internal/inventory/entry.go`, rewriting to operate on `v1alpha1.InventoryEntry`. Add `LabelComponentName` constant (from `cli/pkg/core/labels.go`).
- [x] 2.2 Write unit tests for identity comparison (version excluded, component included/excluded) and entry construction from unstructured resource

## 3. Stale set computation

- [x] 3.1 Copy `ComputeStaleSet` from `cli/pkg/inventory/entry.go` into `internal/inventory/stale.go`, rewriting to operate on `v1alpha1.InventoryEntry` (replaces empty stub)
- [x] 3.2 Write unit tests for stale set computation (stale entries detected, no stale, version-agnostic identity)

## 4. Digest computation

- [x] 4.1 Copy `ComputeDigest` from `cli/pkg/inventory/entry.go` into `internal/inventory/digest.go`, rewriting to operate on `v1alpha1.InventoryEntry` (replaces empty stub)
- [x] 4.2 Write unit tests for digest computation (determinism across ordering, content sensitivity)

## 5. Validation

- [x] 5.1 Run `make fmt vet lint test` and verify all checks pass
