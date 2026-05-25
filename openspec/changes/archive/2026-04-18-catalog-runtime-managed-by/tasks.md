## 1. Catalog edit + publish (cross-repo: catalog/)

- [x] 1.1 Edit `catalog/core/v1alpha1/transformer/transformer.cue`: add `#runtimeName!: t.#NameType` as a top-level field of `#TransformerContext` (alongside `#moduleReleaseMetadata` and `#componentMetadata`).
- [x] 1.2 Edit `controllerLabels` (currently lines 154-158): replace the literal `"open-platform-model"` value of `"app.kubernetes.io/managed-by"` with a reference to `#runtimeName`.
- [x] 1.3 Remove the `#runtimeLabels` field declaration from `#TransformerContext` (if explicitly declared) and any references to it. Confirm nothing in the `labels` or `annotations` merge blocks references it.
- [x] 1.4 Update doc comment block (currently lines 109-115) to describe `#runtimeName` and remove any reference to `#runtimeLabels`.
- [x] 1.5 Run `task fmt && task vet && task test && task check` in the `catalog/` repo. All must pass.
- [x] 1.6 Bump catalog module version per catalog conventions (likely MINOR — additive mandatory field at the schema level). **Bumped `core/v1alpha1` v1.3.2 → v1.3.4 (patch, auto by `publish:smart`; deviates from proposal's MINOR recommendation — acceptable for local dev registry, consumers pin to exact tag).**
- [x] 1.7 Publish catalog to OCI registry: `task publish:smart` (or equivalent). Capture the new version string for use in tasks 2.x. **Published: `opmodel.dev/core/v1alpha1@v1.3.4` (checksum `0db99345...`).**

## 2. Pin new catalog version in poc-controller

- [x] 2.1 Update `poc-controller/cue.mod/module.cue` (and any subpackages with their own `cue.mod/`) to require the new catalog version published in 1.7. Use the workspace `task deps:update` from the workspace root. **Ran workspace `task deps:update` (bumped `poc-controller/catalog/cue.mod/module.cue`, test fixtures already at v1.3.4 via previous runs). Also updated `internal/synthesis/synthesis.go:16` `CatalogVersion` constant v1.3.2 → v1.3.4 and `test/fixtures/releases/hello/cue.mod/module.cue` v1.3.2 → v1.3.4 (deps:update couldn't resolve because local-only fixture dep was unpublished).**
- [x] 2.2 Verify `task vet` and `task check` in `poc-controller/` pass against the new catalog. If render breaks because `#runtimeName` is missing somewhere, that is the bug task 3.x fixes — proceed. **`task dev:vet` passes — render bug will be fixed in 3.x.**

## 3. Update controller render code

- [x] 3.1 In `internal/render/module.go:94-100`, replace the `cueCtx.CompileString(... #runtimeLabels ...)` block + `raw.FillPath(cue.ParsePath("#runtimeLabels"), ...)` with a single `FillPath` of `#runtimeName` to `core.LabelManagedByControllerValue`. Use `cuecontext.New().Encode(core.LabelManagedByControllerValue)` or the equivalent to produce a `cue.Value` for the fill. **Deviation: removed the top-level FillPath entirely (it never propagated to `#TransformerContext.#runtimeName` — different scope). Per-transform injection via `pkg/render/execute.go:injectContext` is now the single injection point, matching the proposal's "single injection point through runtimeNameOverride" intent. Same cleanup applied to `internal/render/release.go`.**
- [x] 3.2 In `pkg/render/execute.go:243-259`, rename the `runtimeLabels` / `runtimeLabelsOverride` mechanism to `runtimeName` / `runtimeNameOverride`. Change type from `map[string]string` to `string`. The `FillPath` target moves from `cue.MakePath(cue.Def("context"), cue.Def("runtimeLabels"))` to `cue.MakePath(cue.Def("context"), cue.Def("runtimeName"))` (or wherever the catalog places `#runtimeName` after edit 1.1).
- [x] 3.3 Update `pkg/render/ProcessModuleRelease` signature accordingly: the `controllerLabels` map parameter becomes a single `runtimeName string` parameter (or update the existing parameter's purpose if backward compat across call sites is needed). Trace callers: `internal/render/module.go:113` is the controller call site; the CLI's call site lives in `cli/` and is updated by the CLI sister change. **Also updated internal/render/release.go:89 caller.**
- [x] 3.4 In `internal/render/module.go:109-113`, drop the `core.LabelModuleReleaseNamespace` entry from the controller-side label map — it is part of the orphaned mechanism and never reached the rendered output. (If we want this label to be stamped, that is a separate change to the catalog's `componentLabels` or `moduleLabels`; out of scope here.) **Controller-side map deleted entirely (ProcessModuleRelease now takes a single `runtimeName string`).**
- [x] 3.5 Run `make fmt vet test` in `poc-controller/`. Address any compilation errors. **Ran `task dev:fmt dev:vet dev:test` — all pass.**

## 4. Render-and-check test (the contract enforcer)

- [x] 4.1 Add a new unit test (`pkg/render/render_runtime_label_test.go` or `internal/render/runtime_label_test.go`): construct a minimal `#ModuleRelease` (one component, one resource — a ConfigMap is sufficient). Render it via the production pipeline with `runtimeName = core.LabelManagedByControllerValue`. **Added `test/integration/reconcile/runtime_identity_test.go` — uses hello fixture module + real catalog provider; skips when no local test registry (standard integration-test pattern).**
- [x] 4.2 Assert the rendered resource's `metadata.labels["app.kubernetes.io/managed-by"]` exactly equals `core.LabelManagedByControllerValue`. Use `assert.Equal`, not `Contains`.
- [x] 4.3 Assert the rendered resource's `metadata.labels["module-release.opmodel.dev/uuid"]` is non-empty (sanity check the catalog ownership labels still flow).
- [x] 4.4 Add a negative test: omit `runtimeName` from the render call and assert CUE evaluation returns an error mentioning the missing required field. (May require adding a "no-default" test variant of the render entrypoint, or using a lower-level helper.) **Uses a synthesized tiny CUE package that imports the catalog transformer and unifies `#TransformerContext` without `#runtimeName`; `Validate(Concrete)` surfaces the missing-field error.**
- [x] 4.5 Run the new tests in isolation: `go test ./pkg/render -run TestRender_RuntimeName -v` and `go test ./internal/render -run TestRender_RuntimeName -v`. **Ran `go test ./test/integration/reconcile/ -ginkgo.focus="Runtime identity injection" -v` — 2/2 pass (21 other specs skipped by focus).**

## 5. Validation gates

- [x] 5.1 `make manifests generate` — no diff (no API changes). **Ran `task dev:manifests dev:generate` — no diff to generated files.**
- [x] 5.2 `make fmt vet` — must pass. **`task dev:fmt dev:vet` passes.**
- [x] 5.3 `make lint` — must pass. **`task dev:lint` passes: 0 issues.**
- [x] 5.4 `make test` — full suite must pass. Particular attention to any test that hand-constructs `#TransformerContext`; those need `#runtimeName` filled. **`task dev:test` passes. Two test helpers (`internal/controller/testhelpers_test.go` and `test/integration/reconcile/suite_test.go`) referenced `#context.#runtimeLabels` in inline CUE transformers — both updated to use `#context.#runtimeName` (single string), producing equivalent rendered labels.**
- [x] 5.5 `KUBEBUILDER_ASSETS=$(./bin/setup-envtest use 1.35.0 --bin-dir ./bin -p path) go test ./test/integration/...` — integration suite must pass. **Passes.**
- [x] 5.6 Manual smoke test (optional): apply a sample `ModuleRelease` to a kind cluster, `kubectl get cm -o jsonpath='{.items[*].metadata.labels.app\.kubernetes\.io/managed-by}'`, verify the value is `opm-controller` for newly-applied resources. **Skipped (optional).**
- [x] 5.7 `openspec validate catalog-runtime-managed-by --strict` — must pass. **Passes.**

## 6. Coordinate CLI sister change

- [x] 6.1 Notify CLI repo: catalog version published in 1.7 is now available. The CLI sister change (`cli/openspec/changes/catalog-runtime-managed-by`) should pin to that version and run its own runtime update. **Catalog at `opmodel.dev/core/v1alpha1@v1.3.4` is published. CLI sister change at `cli/openspec/changes/catalog-runtime-managed-by/tasks.md` is unmodified — its own `/opsx:apply` run will pick up v1.3.4 via `task deps:update`.**
- [x] 6.2 Confirm CLI integration tests against the new catalog version before considering the cross-repo migration complete (tracked in CLI sister change). **Deferred — lives in the CLI sister change; cannot be completed from poc-controller.**
