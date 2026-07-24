# Tasks: operator-platform-status-operator-version

## 1. Version identity

- [x] 1.1 Add `internal/version/version.go` (`Version` const with `x-release-please-version` annotation, `Full()` with best-effort VCS suffix) + unit test guarding annotation placement and semver validity
- [x] 1.2 Add `extra-files: ["internal/version/version.go"]` to `release-please-config.json`
- [x] 1.3 Log `version.Full()` at startup in `cmd/main.go`

## 2. Status field + stamp

- [x] 2.1 Add `OperatorVersion` to `PlatformStatus` in `api/v1alpha1/platform_types.go` + `Operator` printcolumn marker; run `task dev:manifests dev:generate`; regenerate `dist/install.yaml`
- [x] 2.2 Stamp `plat.Status.OperatorVersion = version.Full()` on both reconcile exits in `internal/controller/platform_controller.go`
- [x] 2.3 Extend envtest assertions: success path (`platform_controller_test.go`) and failure path (`platform_failure_test.go`) assert `status.operatorVersion == version.Full()`

## 3. Verification

- [x] 3.1 `task dev:test` and `task dev:lint` clean; `grep operatorVersion config/crd/bases/opmodel.dev_platforms.yaml dist/install.yaml` shows field + printcolumn
- [x] 3.2 Note in PR body: verify the next Release PR bumps the constant (generic-updater residual risk, design.md)
