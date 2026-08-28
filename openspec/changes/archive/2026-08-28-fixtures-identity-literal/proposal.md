## Why

All four operator fixtures (`hello`, `hello_web`, `podinfo`, `redis`) carry the two-part workaround the `modules` fleet retired on 2026-08-28: `identity/identity.cue` declares `Version: #VersionType | *"x.y.z"` (a defaulted disjunction the kernel's shape gate refuses as not concrete) and `module.cue` interpolates `version: "\(id.Version)"` to force the default before the gate runs. The fixtures are what the controller's tests load through the kernel at reconcile; they should carry the form the CLI now emits (`cli` change `scaffold-identity-literal`) and the fleet uses, a plain literal referenced plainly, rather than a comment explaining a defect.

## What Changes

- Four `identity/identity.cue`: `Version: "<next patch>"`, no local `#VersionType` (redundant: publish unifies the package against core's `#IdentityPackage`, which already constrains `Version`).
- Four `module.cue`: `version: id.Version`, interpolation comment removed.
- Each fixture's version advances one patch (a fixture is a published artifact; `hack/fixtures.sh check` refuses a changed fixture at a version GHCR holds): `hello` 0.0.6→0.0.7, `hello_web` 0.1.4→0.1.5, `podinfo` 0.1.5→0.1.6, `redis` 0.1.8→0.1.9.
- Each fixture's `opmodel.dev/catalogs/opm@v2` pin advances `v2.0.0-alpha.6` -> `v2.0.0-alpha.7` (the current release; `task deps:pins:fixtures` performs the bump), in both `test/fixtures/modules/<m>/cue.mod/module.cue` and the sibling `test/fixtures/modulepackages/<m>/cue.mod/module.cue`.
- Consumers re-pinned: `test/fixtures/modulepackages/<m>/cue.mod/module.cue` (`v:`), `test/fixtures/modules/<m>/moduleinstance.yaml` (`version:`), `config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml` (`hello`). The workspace root `task deps:pins:fixtures` performs this sweep.
- `hack/fixtures.sh` and `test/fixtures/fixtures.go` untouched (byte-identical copies of `cli`'s; root `task fixtures:lint` stays green).

Not in scope: Go code, CRDs, controllers; the `cli` podinfo fixture (its own change).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `example-test-modules`: "Fleet composition and identity shape" gains the literal-identity requirement and a scenario.

## Impact

- API types / controllers: none. Reconcile phases: none (Render loads the same fixtures; only the version label in rendered output changes).
- CI: `test.yml` seeds the new versions into the job-local registry from the tree and runs the render coverage against them; `hack/fixtures.sh check` verifies the versions are not on GHCR; `publish-fixtures.yml` publishes them on merge; `test-e2e.yml` pins examples to the published pre-release afterwards.
- SemVer: none (`test(fixtures)`, no release).
- Enhancement: none.
