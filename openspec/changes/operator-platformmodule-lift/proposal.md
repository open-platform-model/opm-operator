## Why

`internal/platformmodule` was written for 0019 D6 as the first platform-module generator, and its design deferred lifting the generator into the library until a second frontend needed it. That lift is now `library-platform-module-generator` (`opm/helper/platformmodule`: `Generate`, `Roots`, `Closure`, `Files.WriteTo`, byte-identical output for the same input). Keeping the operator's copy would leave two implementations of the D13 closure that must never diverge, plus a core-pin constant (`CoreVersion`) bumped by workspace tooling that can drift from the release the library's render glue was actually verified against.

## What Changes

- Delete `internal/platformmodule/{doc,generate,closure}.go` and their tests (`generate_test.go`, `closure_test.go`, `registry_test.go`). The controller, `cmd/main.go` and the integration tests import the library helper instead. `Layout` (per-generation directories, staging swap, retention, boot reset) is operator process policy and stays; it moves beside the store into `internal/platform` as `layout.go` so the `platformmodule` directory disappears entirely.
- Drop the `CoreVersion` constant: the generated module pins core at the library's verified release (`schema.DefaultSchemaModule`), which is the same value the render build's promotion and skew check are measured against. `ModulePath` (`opmodel.dev/platforms/cluster@v0`) stays an operator constant, passed as generator input.
- The module-file source is constructed with the operator's registry mapping, client type `opm-operator` and the process environment passed explicitly (the helper reads no environment itself).
- Bump `go.mod` to the library release carrying the helper.
- Workspace-root follow-through (named here, done under the root `Taskfile`, not this repo): the `.tasks/deps/platform-pins.sh` stanza that rewrites `CoreVersion` is removed; the sample-Platform stanza stays.

Behaviour is unchanged: same files on disk for the same CR, same conditions, same directory lifecycle. The one observable difference is the core pin's provenance, which the `platform-module-generation` spec states.

## SemVer classification

PATCH by behaviour. Commit type `refactor` releases under this repo's rules (the image changes), which is intended: the release is the one that pins core from the library.

## Complexity justification

Net deletion (about 350 lines of code plus about 470 of tests) against one import. No new abstraction.

## Affected API types and controllers

- API: none. `PlatformSpec` and `Subscription` unchanged; no CRD regeneration.
- Controllers: `PlatformReconciler` (`internal/controller/platform_controller.go`: `modFiles`, `platformEntries`, the generate-write-build sequence), `cmd/main.go` (layout construction, file-name constants).
- Tests: `internal/controller/platform_controller_test.go`, `test/integration/reconcile/{platform_recovery,registry_helpers}_test.go`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-module-generation`: the core pin is the library's verified release rather than an operator compiled-in version; generation goes through the shared library helper.

## Impact

- Deleted: `internal/platformmodule/` (after `layout.go` moves).
- Modified: `internal/controller/platform_controller.go`, `internal/platform/layout.go` (moved), `cmd/main.go`, the three test files above, `go.mod`/`go.sum`, `CLAUDE.md` layout notes if they name the package.
- Depends on: `library` release with `opm/helper/platformmodule` (`library-platform-module-generator`).
- Not an enhancement decision carrier: 0019 D6 is already logged by `operator-platform-module-generation`; no `enhancement.yaml`.
