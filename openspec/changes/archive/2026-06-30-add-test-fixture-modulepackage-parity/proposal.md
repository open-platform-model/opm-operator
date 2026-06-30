## Why

The operator's `test/fixtures/` exercise two deployment paths: **ModuleInstance** (the
controller synthesizes the module from the registry; values on the CR) and **ModulePackage**
(Flux fetches an authored `instance.cue` artifact; values in the package). Coverage is
currently lopsided: only `hello` has a `modulepackages/` fixture, so the ModulePackage
render path (`KernelPackageRenderer`) is exercised by exactly one module, and `hello-web`
is the only test module with no `moduleinstance.yaml`, so it can't be applied as a direct
instance. While mapping the fixtures we also found two stale artifacts: `.tasks/release.yaml`
is hardcoded to a non-existent `test/fixtures/releases/hello/release.yaml`, and
`test/fixtures/modules/README.md` still references `modulerelease.yaml` (renamed to
`moduleinstance.yaml` in enhancement 0002).

This is test-fixture, test, and tooling work only — **no controller behavior, API type, or
RBAC marker changes**. The only spec deltas extend the existing `example-test-modules`
capability with the fixture-coverage requirements this change satisfies.

## What Changes

- **New ModulePackage fixtures** for `hello-web`, `podinfo`, `redis` under
  `test/fixtures/modulepackages/<name>/`, each a 5-file clone of the existing `hello/`
  fixture (`cue.mod/module.cue`, `instance.cue`, `values.cue`, `modulepackage.yaml`,
  `ocirepository.yaml`) with per-module path/version/values substituted.
- **New `moduleinstance.yaml`** for `hello-web` under `test/fixtures/modules/hello-web/`,
  mirroring `podinfo`'s Deployment-rendering RBAC bundle, bringing all four modules to
  ModuleInstance parity.
- **Table-driven render test**: `test/integration/reconcile/kernel_package_renderer_test.go`
  loops the "materialized platform" case over all four modulepackages instead of only
  `hello`, reusing the existing platform-materialization and per-resource assertions.
- **Tooling fix**: `.tasks/release.yaml` parameterized over a `PKG` var (default `hello`)
  with corrected paths (`modulepackages/<pkg>/modulepackage.yaml`), plus a `publish:all`
  convenience target.
- **Docs fix**: `test/fixtures/modules/README.md` corrected (`modulerelease.yaml` →
  `moduleinstance.yaml`) and `hello-web` added to the apply-an-example section.

## Capabilities

### Modified Capabilities
- `example-test-modules`: adds requirements that every example module ship a ModulePackage
  fixture, that `hello-web` ship a ready-to-apply `ModuleInstance`, and that the
  `KernelPackageRenderer` integration test cover every modulepackage fixture. No controller
  runtime behavior changes — these requirements describe the test-fixture suite itself.

## Impact

- **API**: none.
- **RBAC**: none (controller markers unchanged; the new fixtures carry their own
  per-instance `ServiceAccount`/`Role`/`RoleBinding`, consistent with existing fixtures).
- **Code**: `test/integration/reconcile/kernel_package_renderer_test.go` (table-driven).
- **Fixtures**: new dirs under `test/fixtures/modulepackages/{hello-web,podinfo,redis}/`;
  new `test/fixtures/modules/hello-web/moduleinstance.yaml`.
- **Tooling/Docs**: `.tasks/release.yaml`, `test/fixtures/modules/README.md`.
- **Compatibility**: additive; existing fixtures and the `hello` paths are unchanged.
  SemVer: PATCH (test/tooling only).
