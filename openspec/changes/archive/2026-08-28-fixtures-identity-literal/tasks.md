## 1. test/fixtures/modules (four fixtures: hello, hello_web, podinfo, redis)

- [x] 1.1 `identity/identity.cue`: `Version: "<current>"` literal; drop `#VersionType` and its comment.
- [x] 1.2 `module.cue`: `version: id.Version`; drop the interpolation comment.
- [x] 1.3 `opm module version set` to the next patch on each (`hello` 0.0.7, `hello_web` 0.1.5, `podinfo` 0.1.6, `redis` 0.1.9).

## 2. Consumers

- [x] 2.1 From the workspace root, `task deps:pins:fixtures`; confirm the diff touches only the catalog pin (`v2.0.0-alpha.7`) in `test/fixtures/modules/*/cue.mod/module.cue` and version pins in `test/fixtures/modulepackages/*/cue.mod/module.cue`, `test/fixtures/modules/*/moduleinstance.yaml` and `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml`.

## 3. Validation gates

- [x] 3.1 `hack/fixtures.sh check` passes (gates plus "version not on GHCR").
- [x] 3.2 `task dev:test:seeded` (seed + render coverage under a materialized platform) passes.
- [x] 3.3 From the workspace root, `task fixtures:lint` stays green.
- [x] 3.4 `task dev:fmt dev:vet dev:lint dev:test`.
