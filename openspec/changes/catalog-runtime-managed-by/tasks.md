## 1. Catalog edit + publish (cross-repo: catalog/)

- [ ] 1.1 Edit `catalog/core/v1alpha1/transformer/transformer.cue`: add `#runtimeName!: t.#NameType` as a top-level field of `#TransformerContext` (alongside `#moduleReleaseMetadata` and `#componentMetadata`).
- [ ] 1.2 Edit `controllerLabels` (currently lines 154-158): replace the literal `"open-platform-model"` value of `"app.kubernetes.io/managed-by"` with a reference to `#runtimeName`.
- [ ] 1.3 Remove the `#runtimeLabels` field declaration from `#TransformerContext` (if explicitly declared) and any references to it. Confirm nothing in the `labels` or `annotations` merge blocks references it.
- [ ] 1.4 Update doc comment block (currently lines 109-115) to describe `#runtimeName` and remove any reference to `#runtimeLabels`.
- [ ] 1.5 Run `task fmt && task vet && task test && task check` in the `catalog/` repo. All must pass.
- [ ] 1.6 Bump catalog module version per catalog conventions (likely MINOR — additive mandatory field at the schema level).
- [ ] 1.7 Publish catalog to OCI registry: `task publish:smart` (or equivalent). Capture the new version string for use in tasks 2.x.

## 2. Pin new catalog version in poc-controller

- [ ] 2.1 Update `poc-controller/cue.mod/module.cue` (and any subpackages with their own `cue.mod/`) to require the new catalog version published in 1.7. Use the workspace `task update-deps` from the workspace root.
- [ ] 2.2 Verify `task vet` and `task check` in `poc-controller/` pass against the new catalog. If render breaks because `#runtimeName` is missing somewhere, that is the bug task 3.x fixes — proceed.

## 3. Update controller render code

- [ ] 3.1 In `internal/render/module.go:94-100`, replace the `cueCtx.CompileString(... #runtimeLabels ...)` block + `raw.FillPath(cue.ParsePath("#runtimeLabels"), ...)` with a single `FillPath` of `#runtimeName` to `core.LabelManagedByControllerValue`. Use `cuecontext.New().Encode(core.LabelManagedByControllerValue)` or the equivalent to produce a `cue.Value` for the fill.
- [ ] 3.2 In `pkg/render/execute.go:243-259`, rename the `runtimeLabels` / `runtimeLabelsOverride` mechanism to `runtimeName` / `runtimeNameOverride`. Change type from `map[string]string` to `string`. The `FillPath` target moves from `cue.MakePath(cue.Def("context"), cue.Def("runtimeLabels"))` to `cue.MakePath(cue.Def("context"), cue.Def("runtimeName"))` (or wherever the catalog places `#runtimeName` after edit 1.1).
- [ ] 3.3 Update `pkg/render/ProcessModuleRelease` signature accordingly: the `controllerLabels` map parameter becomes a single `runtimeName string` parameter (or update the existing parameter's purpose if backward compat across call sites is needed). Trace callers: `internal/render/module.go:113` is the controller call site; the CLI's call site lives in `cli/` and is updated by the CLI sister change.
- [ ] 3.4 In `internal/render/module.go:109-113`, drop the `core.LabelModuleReleaseNamespace` entry from the controller-side label map — it is part of the orphaned mechanism and never reached the rendered output. (If we want this label to be stamped, that is a separate change to the catalog's `componentLabels` or `moduleLabels`; out of scope here.)
- [ ] 3.5 Run `make fmt vet test` in `poc-controller/`. Address any compilation errors.

## 4. Render-and-check test (the contract enforcer)

- [ ] 4.1 Add a new unit test (`pkg/render/render_runtime_label_test.go` or `internal/render/runtime_label_test.go`): construct a minimal `#ModuleRelease` (one component, one resource — a ConfigMap is sufficient). Render it via the production pipeline with `runtimeName = core.LabelManagedByControllerValue`.
- [ ] 4.2 Assert the rendered resource's `metadata.labels["app.kubernetes.io/managed-by"]` exactly equals `core.LabelManagedByControllerValue`. Use `assert.Equal`, not `Contains`.
- [ ] 4.3 Assert the rendered resource's `metadata.labels["module-release.opmodel.dev/uuid"]` is non-empty (sanity check the catalog ownership labels still flow).
- [ ] 4.4 Add a negative test: omit `runtimeName` from the render call and assert CUE evaluation returns an error mentioning the missing required field. (May require adding a "no-default" test variant of the render entrypoint, or using a lower-level helper.)
- [ ] 4.5 Run the new tests in isolation: `go test ./pkg/render -run TestRender_RuntimeName -v` and `go test ./internal/render -run TestRender_RuntimeName -v`.

## 5. Validation gates

- [ ] 5.1 `make manifests generate` — no diff (no API changes).
- [ ] 5.2 `make fmt vet` — must pass.
- [ ] 5.3 `make lint` — must pass.
- [ ] 5.4 `make test` — full suite must pass. Particular attention to any test that hand-constructs `#TransformerContext`; those need `#runtimeName` filled.
- [ ] 5.5 `KUBEBUILDER_ASSETS=$(./bin/setup-envtest use 1.35.0 --bin-dir ./bin -p path) go test ./test/integration/...` — integration suite must pass.
- [ ] 5.6 Manual smoke test (optional): apply a sample `ModuleRelease` to a kind cluster, `kubectl get cm -o jsonpath='{.items[*].metadata.labels.app\.kubernetes\.io/managed-by}'`, verify the value is `opm-controller` for newly-applied resources.
- [ ] 5.7 `openspec validate catalog-runtime-managed-by --strict` — must pass.

## 6. Coordinate CLI sister change

- [ ] 6.1 Notify CLI repo: catalog version published in 1.7 is now available. The CLI sister change (`cli/openspec/changes/catalog-runtime-managed-by`) should pin to that version and run its own runtime update.
- [ ] 6.2 Confirm CLI integration tests against the new catalog version before considering the cross-repo migration complete (tracked in CLI sister change).
