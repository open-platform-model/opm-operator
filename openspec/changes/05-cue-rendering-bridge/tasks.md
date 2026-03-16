## 1. RenderResult type and module loading

- [ ] 1.1 Define `RenderResult` struct with `Resources []*core.Resource` and `InventoryEntries []v1alpha1.InventoryEntry` in `internal/render/module.go`
- [ ] 1.2 Implement CUE module loading from directory via `loader.LoadModulePackage`
- [ ] 1.3 Implement module metadata and `#config` schema extraction from loaded value

## 2. Values conversion and rendering

- [ ] 2.1 Implement `RawValues` to `cue.Value` conversion via `cueCtx.CompileBytes`
- [ ] 2.2 Wire `module.ParseModuleRelease` with converted values
- [ ] 2.3 Wire `module.Process` with parsed release and provider (relocated from `render.ProcessModuleRelease` in change 1)
- [ ] 2.4 Implement runtime labels injection (`#runtimeLabels` with `managed-by: opm-controller`)

## 3. Inventory entry construction

- [ ] 3.1 Convert rendered `[]*core.Resource` to `[]v1alpha1.InventoryEntry` using the inventory bridge

## 4. Tests

- [ ] 4.1 Create a minimal CUE module test fixture (module with one component producing a ConfigMap)
- [ ] 4.2 Write unit tests: successful render, invalid values, nil values, runtime label presence
- [ ] 4.3 Write unit test verifying inventory entries match rendered resources

## 5. Validation

- [ ] 5.1 Run `make fmt vet lint test` and verify all checks pass
