## 1. Shim writer

- [x] 1.1 Create `internal/moduleacquire/shim.go`: write a temp dir with `cue.mod/module.cue` (one dep: `<path>@<version>`; appropriate `language.version`) and a package `.cue` that imports `<path>` and embeds it at the root
- [x] 1.2 Ensure no catalog path/version appears in either generated file
- [x] 1.3 Unit test asserting emitted `cue.mod/module.cue` (single dep, no catalog) and `.cue` (import + root embed) content

## 2. Acquire

- [x] 2.1 `internal/moduleacquire/acquire.go`: `Acquire(ctx, k *kernel.Kernel, path, version, registry string) (*module.Module, error)` — write shim, `defer os.RemoveAll`, `k.LoadModulePackage(ctx, dir, file.LoadOptions{Registry: registry})`, `k.NewModuleFromValue(v)`
- [x] 2.2 Wrap errors with context (`acquiring module %q@%q: %w`); ensure temp dir is removed on every path (success + error)

## 3. Tests + validation gates

- [x] 3.1 Integration test (reuse `test-registry-lifecycle` + a published module fixture; skip under `-short`/unreachable registry): acquire the module, assert decoded `module.Module` metadata (name, version)
- [x] 3.2 Integration test: unresolvable path/version returns an error; assert no temp dir leak (check temp root before/after)
- [x] 3.3 `task dev:fmt dev:vet`
- [x] 3.4 `task dev:lint`
- [x] 3.5 `task dev:test`
