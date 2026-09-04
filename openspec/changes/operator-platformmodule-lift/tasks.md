# Tasks: operator-platformmodule-lift

Gate: a published `library` release carrying `opm/helper/platformmodule` (`library-platform-module-generator`).

## 1. Dependencies

- [x] 1.1 Bump `go.mod` to the library release with the helper; `go mod tidy`.

## 2. Internal Packages

- [x] 2.1 Move `internal/platformmodule/layout.go` and `layout_test.go` to `internal/platform/layout.go` (package `platform`, beside the store); keep the `Layout` API and the `gen-`/`.staging-`/`.aside-` naming unchanged.
- [x] 2.2 Add a temporary equivalence test generating the sample Platform's input (`config/samples/opmodel.dev_v1alpha1_platform.yaml` entries, core pinned to the helper's default) through the in-tree generator and the library helper, asserting byte-equal `cue.mod/module.cue` and `platform.cue` (from line 2 on: line 1 is the generator attribution header, which the library deliberately changed so a shared helper names no frontend); run it green.
- [x] 2.3 Delete `internal/platformmodule/{doc,generate,closure}.go`, `generate_test.go`, `closure_test.go`, `registry_test.go` and the equivalence test; remove the directory.

## 3. Controller

- [x] 3.1 `internal/controller/platform_controller.go`: import the library helper; `platformEntries` returns helper `Entry` values; `modFiles` builds `platformmodule.NewRegistry(RegistryConfig{Registry, ClientType: "opm-operator", Env: os.Environ()})`; the reconcile calls `Roots` (no core override), `Closure`, `Generate(Input{Name, Type, ModulePath: opmodel.dev/platforms/cluster@v0, Entries, Deps})` and `Layout.Write` unchanged; keep the operator-side `ModulePath` constant next to the reconciler.
- [x] 3.2 `cmd/main.go`: construct `platform.Layout`; replace file-name constants with the helper's.
- [x] 3.3 Update `internal/controller/platform_controller_test.go` and `test/integration/reconcile/{platform_recovery,registry_helpers}_test.go` to the helper's identifiers; add the "Core pin follows the library" assertion (generated module file pins core at `schema.DefaultSchemaModule`'s version).

## 4. Docs and tooling

- [x] 4.1 `CLAUDE.md` / `docs/RENDERING.md`: replace any mention of the in-tree generator or `CoreVersion` with the library helper and the library-owned core pin.
- [x] 4.2 Workspace root (outside this repo, done in the same working session): delete the `CoreVersion` stanza from `.tasks/deps/platform-pins.sh` and its header line; keep the sample-Platform catalog stanzas; update the root `CLAUDE.md` `deps:update` description if it names the constant.

## 5. Verification

- [x] 5.1 `task dev:fmt dev:vet dev:lint dev:test` green; `task dev:test:seeded` for the registry-backed controller specs.
- [x] 5.2 `go run ./cmd` smoke: with a Platform CR present, the generated `gen-<n>/cue.mod/module.cue` pins core at the library's default version and the CR reports Ready=True reason `Generated`.
