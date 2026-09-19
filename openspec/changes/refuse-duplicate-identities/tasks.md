# Tasks: refuse-duplicate-identities

Two sections per design.md. design.md carries no unverified assumption: the adapter and both classifiers were read at the current commit, and the helper's API was read at the released version, so section 1 is not a spike.

## 1. Pin, reason and refusal (go.mod, internal/status, internal/render)

- [x] 1.1 `go get github.com/open-platform-model/library@v1.0.0-alpha.33 && go mod tidy`, then `go build ./...`. Verify: `go.mod` names alpha.33 and `go doc github.com/open-platform-model/library/opm/helper/objectset` lists `Duplicates` and `DuplicateIdentitiesError`.
- [x] 1.2 Add `DuplicateIdentitiesReason = "DuplicateIdentities"` to `internal/status/conditions.go` with the doc comment from design.md § The reason. Verify: `go vet ./internal/status/...` passes.
- [x] 1.3 In `internal/render/kernel_module_renderer.go`, make `resultFromRender` call `objectset.Duplicates(out.Compiled)` first and return `&objectset.DuplicateIdentitiesError{Duplicates: dups}` bare when non-empty, before any resource, inventory entry or warning is built; note in the function's doc that the check runs first and why (design.md § The check sits first in the adapter). Verify: `go build ./... && go vet ./...` pass.
- [x] 1.4 Adapter tests beside `warnings_test.go`, with `cuecontext.New().CompileString` values: two `TransformerRegistration` objects with one name from components `registration` and `registration-copy` refuse with an error that `errors.AsType` finds as `*objectset.DuplicateIdentitiesError`, whose message names the identity once and both components with their transformers, and a nil result; two objects with distinct names adapt to two resources and two inventory entries; a Deployment rendered twice beside a distinct Service refuses naming only the Deployment. Verify: `go test ./internal/render/...` passes.
- [ ] 1.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(render): refuse a render whose objects share one apply identity`.

## 2. Classification and docs (internal/reconcile, docs)

- [ ] 2.1 In `internal/reconcile/resolution.go`, add `isDuplicateIdentities` and the `DuplicateIdentitiesReason` arm to `renderFailureReason` between the skew and resolution arms; update the function's doc comment to list the four reasons in precedence order. Verify: `resolution_test.go` gains a case where a `*objectset.DuplicateIdentitiesError` (bare, and wrapped once with `%w`) classifies as `DuplicateIdentities`, and the existing skew, resolution and fallback cases pass unchanged.
- [ ] 2.2 Confirm both loops surface it identically: a test in `internal/reconcile` (the pattern the existing classifier tests use) asserts `classifyRenderError` marks `Ready=False`/`Stalled=True` with reason `DuplicateIdentities` and the library's message on a ModuleInstance, and the ModulePackage classifier returns the same reason for the same error. Verify: `go test ./internal/reconcile/...` passes.
- [ ] 2.3 `docs/RENDERING.md`: add the `DuplicateIdentities` row to the Ready-reason table (cause: two rendered objects share one apiVersion, kind, namespace and name; remedy: remove or rename one of the named components) and one sentence in the render section saying the adapter checks identities before anything is applied (enhancement 0015 D15). Verify: the table renders and the reason string matches `internal/status/conditions.go`.
- [ ] 2.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(reconcile): classify a duplicate-identity refusal under its own reason`.
