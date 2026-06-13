# Design: modernize-test-fixtures

## Context

The kernel render pipeline (library `v0.3.0`) resolves the OPM core schema as `opmodel.dev/core@v0` and materializes component transformers from the catalog module `opmodel.dev/catalogs/opm@v0` (authored in the adjacent `catalog_opm` workspace repo, published to GHCR by its CI; locally publishable via its `task publish VERSION=…`, whose default registry mapping is `opmodel.dev=localhost:5000+insecure`).

The operator's registry-backed integration tests load the fixture module `testing.opmodel.dev/modules/hello@v0` through the kernel (`moduleacquire.Acquire` → `Kernel.LoadModulePackage` → `Kernel.NewModuleFromValue`). The fixture is still authored against the retired schema era:

- `test/fixtures/modules/hello/module.cue` imports `opmodel.dev/core/v1alpha1/module@v1`; `components.cue` imports `opmodel.dev/opm/v1alpha1/resources/config@v1`.
- `test/fixtures/releases/hello/release.cue` imports `opmodel.dev/core/v1alpha1/modulerelease@v1`.

Loading the published old-era artifact through the kernel fails with a CUE parse/import error, so `task dev:test:local` is red at `test/integration/reconcile/acquire_test.go`. Separately, the kernel-renderer happy-path spec (`kernel_module_renderer_test.go`) self-skips because its inline platform materialization subscribes to `opmodel.dev/catalogs/opm`, which is not present in the local registry (`localhost:5000` currently holds `opmodel.dev/core` but no `opmodel.dev/catalogs/opm`).

The authoritative kernel-era module example is `library/testdata/modules/web_app` (`m "opmodel.dev/core@v0"`, catalog imports `opmodel.dev/catalogs/opm/{resources,traits,blueprints/...}`, deps `core@v0 v0.4.0` + `catalogs/opm@v0 v0.5.0`, language `v0.17.0`).

## Goals / Non-Goals

**Goals:**

- The published fixture module loads and decodes through the kernel (`acquire_test.go` green).
- The fixture stays minimal: one component rendering exactly one ConfigMap, so stub-based controller tests' assumptions (ConfigMap-only RBAC in `modulerelease.yaml`) keep holding.
- The kernel-renderer happy-path spec stops skipping on a prepared local registry (catalog resolvable at its default `opmodel.dev/catalogs/opm` subscription path).
- `task dev:test:local` exits green end to end.
- Fixture remains repo-local (decision from exploration: self-contained operator test surface; no dependency on workspace `modules/`, which are themselves still old-era).

**Non-Goals:**

- Wiring `OPM_TEST_CATALOG_PATH` into `dev:test:local` / unskipping the platform success-path and recovery specs (follow-up: catalog-test-harness).
- Triage of the nine pre-kernel stub specs (`status_tracking`, `state_recovery`, `change_propagation`).
- Release-renderer happy-path coverage; kernel-era e2e fixtures.
- Changing production code, CRDs, or controllers in any way.
- Auto-starting or auto-publishing the catalog from the operator repo's Taskfile (the catalog has its own stamped publish flow in `catalog_opm`; we document the prerequisite instead of duplicating the mechanism).

## Decisions

### D1 — Author the fixture against `core@v0` + `catalogs/opm@v0`, modeled on `library/testdata/modules/web_app`

`module.cue` embeds `m.#Module` (`m "opmodel.dev/core@v0"`) with `metadata: {modulePath, name: "hello", version: "0.0.2", description}`, a `#config: {message: string | *"hello from opm"}`, and `debugValues`. `components.cue` attaches the catalog's `#ConfigMaps` component (`res "opmodel.dev/catalogs/opm/resources"`) with `spec: configMaps: hello: data: message: #config.message`.

*Why this shape:* the catalog's `#ConfigMapTransformer` requires only the ConfigMaps resource FQN and has `requiredLabels: {}`, so the minimal module matches a transformer without any workload-type labeling — the smallest possible kernel-era render. Rendered ConfigMap names follow the transformer's `{releaseName}-{componentName}-{cmName}` convention; no test currently asserts a rendered resource name, only provenance/labels/counts, so the naming change is absorbed.

*Alternative considered:* mirroring `web_app`'s stateless-workload blueprint — rejected: drags in Deployment/Service transformers, breaks the ConfigMap-only RBAC fixture, and tests nothing extra at this tier.

### D2 — Bump fixture version to `v0.0.2`; do not republish `v0.0.1`

The local registry and developers' CUE module caches (`~/.cache/cue/mod/extract/...@v0.0.1`) already hold the old-era `v0.0.1` artifact. Republishing the same version invites tag-overwrite ambiguity and stale-cache poisoning. A patch bump makes the new artifact unambiguous everywhere.

Touched references: `acquire_test.go` (version argument and `mod.Metadata.Version` assertion → `"0.0.2"`), `kernel_module_renderer_test.go` (render coordinates), `test/fixtures/modules/hello/modulerelease.yaml` (`spec.module.version`), `.tasks/module.yaml` `MODULE_VERSION` default if pinned, and the `test-registry-lifecycle` spec delta.

*Alternative considered:* keep `v0.0.1` + require registry/cache reset — rejected: every dev machine needs manual surgery, and failure mode (parse error from a stale extract) is obscure.

### D3 — Both kernel-era deps are documented prerequisites; a v0.17 `cue` CLI is required *(amended during implementation)*

`task dev:test:local` requires **both** `opmodel.dev/core@v0` and `opmodel.dev/catalogs/opm@v0` in the local registry at the versions the fixture's `cue.mod` pins. The original design assumed `core@v0` was already local; it was not (the local `opmodel.dev/core` repo held only `v1.0.x` tags). Both are now published to `localhost:5000`: `core@v0 v0.4.0` from the `core/` repo (`task publish VERSION=v0.4.0`) and `catalogs/opm@v0 v0.5.0` from `catalog_opm` (`task publish VERSION=v0.5.0`, whose default `CUE_LOCAL_REGISTRY` targets `localhost:5000+insecure`). Both repos were clean at their respective tags, so the local artifacts reproduce the released content.

Publishing (and `cue mod tidy`/`vet` of the kernel-era fixture) **requires a `cue` CLI at the kernel's language version (v0.17.x)**. The catalog and core declare `language: v0.17.0`; the workspace's stock `cue` v0.16.1 refuses to read them (`language version "v0.17.0" … too new`). The operator's `.tasks/module.yaml` prerequisites block now documents both publish steps and the v0.17 requirement.

*Why not automate from this repo:* the catalog/core publishes are version-stamped and owned by their repos; duplicating them here couples repos and violates Simplicity (Principle VII). Documented one-time steps match the existing operator-driven registry lifecycle model.

### D4 — Pin fixture deps to the published versions *(resolved)*

`cue.mod/module.cue` pins `opmodel.dev/core@v0 v0.4.0` and `opmodel.dev/catalogs/opm@v0 v0.5.0` (resolved via `cue mod tidy` with the task-local publish registry mapping; language stays `v0.16.1` on the fixture — tidy does not bump the consuming module's declared language, and the v0.17 CLI loads it fine).

### D5 — Rewrite `releases/hello` *(superseded — descoped to `fix-moduleacquire-core-v0`)*

Original intent: move `releases/hello/release.cue` to the core@v0 `#ModuleRelease`. **Superseded during implementation.** A hand-authored core@v0 release package (`#module: hello`) fails `cue vet` with `#module.metadata.modulePath: field not allowed` — the same self-referential `#Module` admission failure that blocks the module-acquisition shim (see D6). The canonical hand-authored core@v0 release-package shape is unresolved (the kernel's `LoadReleasePackage` test only uses a `#module: {kind: "Module"}` stub, never a real module), and the Release happy-path has no test harness. The fixture is reverted to old-era and moved to the follow-up change, which must resolve the self-reference loading story before the release fixture can be modernized.

### D6 — `dev:test:local` is blocked by a production bug, not the fixture *(discovered during implementation)*

The modernized module fixture is correct: `cue eval . --concrete` in its own package resolves full metadata (`fqn`, `uuid`). But `acquire` and the `KernelModuleRenderer` happy-path fail because `internal/moduleacquire/shim.go` cannot load a core@v0 `#Module`. The shim writes a throwaway package that embeds the target via `import mod "…/hello@v0"; mod`; that **re-evaluates** `#Module`, and core@v0's self-referential `metadata.modulePath`/`version` collapse to bottom and are rejected as `field not allowed`. `library/opm/helper/synth/release.go` documents this exact constraint and works around it in Go (a `userModule` scope value compiled via `cue.Scope`, never a re-emitted source fragment). The acquisition shim has no equivalent workaround — and there is no kernel "load module from registry by path" API; the shim exists to bridge that gap.

This was masked before this change by the old fixture's parse error. It is a pre-existing production bug, exposed (not caused) by the modernization. **It is out of scope here** — the fix is production kernel-integration work with its own design and tests, tracked in `fix-moduleacquire-core-v0`. This change's acceptance is therefore CI-parity green (`task dev:test`) plus a correct, concrete-evaluable kernel-era fixture; full `dev:test:local` green lands with the follow-up.

## Risks / Trade-offs

- [Catalog version drift: fixture pins `catalogs/opm@v0 vX`, catalog repo publishes ahead, local registry holds only newer] → CUE resolves exact pinned versions; the prerequisite doc says to publish the pinned version (or re-tidy the fixture). The `skipIfNoTestRegistry` message names the catalog so the failure mode is a clear skip/instruction, not a cryptic parse error.
- [`cue mod tidy`/`publish` from the operator repo needs `opmodel.dev=localhost` mapping; a shell-exported `CUE_REGISTRY` could leak] → already mitigated by the existing task-local `PUBLISH_REGISTRY` pattern in `.tasks/module.yaml`; keep it.
- [Renderer happy path starts actually running where it previously skipped — may surface latent kernel-path bugs on dev machines] → that is the point; if it finds real bugs they become their own changes, not scope creep here.
- [Old `v0.0.1` artifact remains in local registries] → harmless; nothing references it after the bump.
- [`hello` rendered ConfigMap name changes (`{release}-{component}-{cm}`)] → no current assertion depends on the rendered name; `modulerelease.yaml`'s RBAC is name-agnostic (resource-type scoped).

## Migration Plan

1. Land fixture rewrite + version bump + test-coordinate updates + `.tasks` docs in one commit (test-and-tooling only; `task dev:test` stays green because gated specs skip in CI).
2. Dev machines: `task registry:start` → publish `core@v0` + `catalogs/opm@v0` (v0.17 cue) → `task module:publish` (now `v0.0.2`). `dev:test:local` is partially green until `fix-moduleacquire-core-v0` lands.
3. Rollback: revert the commit; old fixture and `v0.0.1` artifact are untouched in registries. The locally-published `core@v0`/`catalog@v0` artifacts are harmless to leave.

## Open Questions

- None blocking this change. The moduleacquire core@v0 loading fix (and the dependent release-fixture rewrite) are owned by `fix-moduleacquire-core-v0`. Whether `dev:test:local` should auto-set `OPM_TEST_CATALOG_PATH` stays deferred to the catalog-test-harness follow-up.
