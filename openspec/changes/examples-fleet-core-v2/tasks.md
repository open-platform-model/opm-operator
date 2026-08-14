# Tasks — examples-fleet-core-v2

> Depends on `operator-library-retarget` (merged). Fixture/pipeline work only; the interim-red registry-backed tiers go green here. The release cut after this change is the slice's graduation gate. Commit messages: mind the bare-`@` ban (glue majors to paths).

## 1. Fixture crossing

- [x] 1.1 `hello`: cue.mod deps → core v2 + catalogs/opm v2; imports (`module.cue`, `components.cue` resources → `v1beta1`); `metadata.modulePath: "opmodel.dev/modules/test/hello@v0"`; `version: "0.0.5"`.
- [x] 1.2 `hello_web`: rename directory from `hello-web/`; cue.mod module line → `opmodel.dev/modules/test/hello_web@v0`; same crossing (blueprint import → `v1beta1`); `metadata.name: "hello_web"`, full-address `modulePath`; drop hand-authored workload-type label; `version: "0.1.3"` (first tag on the renamed path).
- [x] 1.3 `podinfo`: same crossing; blueprint + trait imports → `v1beta1`; drop hand-authored workload-type label; `version: "0.1.4"`.
- [x] 1.4 `redis`: same crossing; `version: "0.1.7"`.
- [x] 1.5 `moduleinstance.yaml` pins: `hello v0.0.5`, `hello_web` → new coordinate `opmodel.dev/modules/test/hello_web@v0 v0.1.3`, `podinfo v0.1.4` (fixes the stale `v0.1.2`), `redis v0.1.7`; `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml` → `hello v0.0.5`.
- [x] 1.6 `cue vet`/render each fixture locally against the v2 catalog (GHCR) before any publish.

## 2. hello-web → hello_web rename residue

- [x] 2.1 `.tasks/release.yaml` `PKGS: "hello hello_web podinfo redis"`; `kernel_package_renderer_test.go:125` loop; sweep remaining `hello-web` references (`hack/kind-opm-dev-test/` is historical — leave, note as such).
- [x] 2.2 Record in the change and in 0010's history: the source-side rename delivers half of 0011 D17 item 2; the registry-side residue (old `hello-web@v0` artifacts + `-e2e` tags relocation/deletion) stays with 0011 `registry-cleanup` — note it in that slice's concern when recording.

## 3. modulepackages mirrors

- [x] 3.1 All four: cue.mod core dep → v2; embedded test-module pins → bumped versions; `instance.cue` core import → v2. `hello-web` mirror renames to `releases/test/hello_web@v0`; its instance import drops the explicit `:hello_web` qualifier.
- [x] 3.2 `examples:bundle` `RELEASES_DIR` → `test/fixtures/modulepackages`; verify `dist/opm-examples.tar.gz` now contains the OCIRepository/ModulePackage manifests.

## 4. Tests green

- [x] 4.1 `acquire_test.go:70` → full-address assertion; version pins per design (`hello v0.0.5`, `redis v0.1.7`).
- [x] 4.2 Registry-backed integration tier green locally against a local registry (`task dev:e2e:local` path or `OPM_TEST_REGISTRY_FORCE`); e2e podinfo/redis/lifecycle specs green in kind.
- [x] 4.3 `.tasks/examples.yaml`: comment documenting the grep version-source contract against the v2 metadata shape.

## 5. Spec deltas + validation gates

- [x] 5.1 Spec deltas: `example-test-modules` (fleet = hello/hello_web/podinfo/redis, v2 identity shape, hyphenated-name retirement note), `example-module-publishing` (version-bump-on-crossing requirement, bundle directory fix).
- [x] 5.2 `task dev:manifests dev:generate` (no drift expected), `task dev:fmt dev:vet`, `task dev:lint`, `task dev:test`.

## 6. Record, publish, release

- [ ] 6.1 Push to main → e2e workflow publishes `-e2e.g<sha>` prerelease tags of the v2 fixtures to GHCR — the end-to-end publish proof before the release.
- [ ] 6.2 Record in `enhancements/0010/`: slice `operator-library-retarget` → `done`, `openspec_ref` citing both changes; history event covering the CRD absorption, the `hello_web` source-side rename (partial 0011 D17 item 2 delivery), and the fixture version bumps (new podinfo coordinate `v0.1.4` — the value the CLI track pins).
- [ ] 6.3 Cut the release (with `operator-library-retarget` already in): verify the release publishes `hello v0.0.5` / `hello_web v0.1.3` / `podinfo v0.1.4` / `redis v0.1.7` to GHCR and uploads the v2-CRD `install.yaml` + corrected examples bundle. Notify the CLI track: `operator:sync` target version + the `v0.1.4` pin.
