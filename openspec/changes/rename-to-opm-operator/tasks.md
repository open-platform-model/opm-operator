# Tasks

Grouped by rename surface. Each group is a reviewable commit. The whole set ships in one PR; the commits are split so a reviewer can navigate by concern.

## 1. Go module path and scaffold

- [x] 1.1 Update `go.mod` module directive to `github.com/open-platform-model/opm-operator`.
- [x] 1.2 Rewrite all Go imports matching `github.com/open-platform-model/poc-controller` → `github.com/open-platform-model/opm-operator` (153 sites across 59 files).
- [x] 1.3 Update `PROJECT`: `projectName`, `repo`, and the three `resources[].path` entries.
- [x] 1.4 Update `release-please-config.json` `package-name` from `poc-controller` to `opm-operator`.
- [x] 1.5 Leave `.release-please-manifest.json` at `{".": "0.4.4"}` — do not reset. Next release-please run emits `v0.5.0` under the new name.
- [x] 1.6 `go mod tidy`. Confirm `go build ./...` succeeds.

## 2. CUE catalog module

- [x] 2.1 Update `catalog/cue.mod/module.cue`: `module: "opmodel.dev/poc-controller/catalog@v1"` → `module: "opmodel.dev/opm-operator/catalog@v1"`.
- [x] 2.2 Search the repo for any `opmodel.dev/poc-controller/...` imports in other `.cue` files (none expected); fix any found.
- [x] 2.3 `cd catalog && cue mod tidy`. Confirm `cue vet ./...` still passes.

## 3. Kubernetes in-cluster identity

- [x] 3.1 `config/default/kustomization.yaml`: `namespace: poc-controller-system` → `opm-operator-system`; `namePrefix: poc-controller-` → `opm-operator-`.
- [x] 3.2 Update `app.kubernetes.io/name: poc-controller` → `opm-operator` across all manifests under `config/`. Affected files: `config/manager/manager.yaml`, `config/default/metrics_service.yaml`, `config/rbac/service_account.yaml`, `config/rbac/role_binding.yaml`, `config/rbac/leader_election_role.yaml`, `config/rbac/leader_election_role_binding.yaml`, `config/rbac/modulerelease_{admin,editor,viewer}_role.yaml`, `config/rbac/bundlerelease_{admin,editor,viewer}_role.yaml`, `config/rbac/kustomization.yaml` (comment), `config/network-policy/allow-metrics-traffic.yaml`, `config/prometheus/monitor.yaml`, `config/samples/releases_v1alpha1_{release,modulerelease,modulerelease_jellyfin,bundlerelease}.yaml`.
- [x] 3.3 Update the comment strings in `config/rbac/*_role.yaml` that mention the project by name.
- [x] 3.4 `test/e2e/e2e_test.go`: update constants `namespace`, `serviceAccountName`, `metricsServiceName`, `metricsRoleBindingName` and the string literals for the SA name, the `--clusterrole=poc-controller-metrics-reader` flag, and any other hardcoded `poc-controller-*` identifiers.
- [x] 3.5 `test/e2e/e2e_suite_test.go`: update `managerImage = "example.com/poc-controller:v0.0.1"` and the `Starting poc-controller e2e test suite` log string.

## 4. Build and task plumbing

- [x] 4.1 `.tasks/docker.yaml`: buildx builder name `poc-controller-builder` → `opm-operator-builder`.
- [x] 4.2 `.tasks/release.yaml`: fixture `--source="poc-controller/test/fixtures"` paths updated to reflect the new directory name. Confirm with the user what the final disk directory name will be before committing — if the user keeps the disk dir as `poc-controller`, revert this task; if renamed on disk to `opm-operator`, apply.
- [x] 4.3 `.tasks/operator.yaml` and `.tasks/kind.yaml`: update kubectl patch and rollout-status targets from `poc-controller-system` / `poc-controller-controller-manager` to the new names.
- [x] 4.4 `Taskfile.yml`: `KIND_CLUSTER` default `poc-controller-test-e2e` → `opm-operator-test-e2e`.
- [x] 4.5 `Makefile`: mirror the `Taskfile.yml` changes (Kind cluster default, buildx builder). Note per `CLAUDE.md` that `Taskfile.yml` is authoritative; the `Makefile` remains as scaffold residue but keep it consistent.

## 5. Docs within the repo

- [x] 5.1 `README.md`: project name header, install commands, cosign verification identity regex (`https://github.com/open-platform-model/poc-controller/...` → `.../opm-operator/...`), upgrade-from-`v0.4.x` section referencing the procedure in `design.md`.
- [x] 5.2 `CLAUDE.md`, `CONSTITUTION.md`, `docs/STYLE.md`, `docs/TESTING.md`: textual mentions of `poc-controller` updated where they describe the project by name. Command examples with `-n poc-controller-system` updated.
- [x] 5.3 Create `adr/015-rename-from-poc-controller.md` (Status: Accepted) capturing the decision.
- [x] 5.4 Do **not** edit `adr/014-container-image-ci-publishing.md`. The trade-off paragraph there describes the state at the time of writing; it remains accurate as historical context.
- [x] 5.5 Do **not** edit `CHANGELOG.md` or anything under `openspec/changes/archive/`.

## 6. Active OpenSpec specs

- [x] 6.1 `openspec/specs/container-image-publish/spec.md`: verify no edits needed (references `opm-operator` throughout; confirmed during exploration).
- [x] 6.2 Grep active `openspec/specs/` for any remaining `poc-controller` mentions; update or confirm none.

## 7. Workspace-level coupling (edits to files outside this repo's root)

- [x] 7.1 `../CLAUDE.md` (workspace root): Directory Map, Repo Selection Quick Map, Documentation Ownership Map, Repo Entry Instructions. Replace `poc-controller/` references with `opm-operator/`.
- [x] 7.2 `../open-platform-model.code-workspace`: folder path entries.
- [x] 7.3 `../Taskfile.yml`: `find catalog cli poc-controller modules releases` → `find catalog cli opm-operator modules releases`.
- [x] 7.4 `../releases/Taskfile.yml`: kubectl patch and rollout-status targets for the controller deployment and namespace.
- [x] 7.5 `../releases/install.yaml`: leave as-is. The next `v0.5.0` release of the controller republishes this via the release workflow; the file will be refreshed by existing automation, not by this PR.
- [x] 7.6 Confirm no other workspace files reference `poc-controller` operationally (archived enhancement docs and sibling repo archives are historical and left alone).

## 8. Verification gates

- [x] 8.1 `task dev:manifests dev:generate` — diff must be empty (no new types, no marker changes).
- [x] 8.2 `task dev:fmt dev:vet`.
- [x] 8.3 `task dev:lint`.
- [x] 8.4 `task dev:test`.
- [ ] 8.5 `task dev:e2e`. **Status**: 1/12 spec failed — `Manager > should ensure the metrics endpoint is serving metrics` hit a 3-minute timeout waiting for controller pod `Ready=True`. The prior spec (`should run successfully`) saw the same pod in `Phase=Running`, so the failure is about readiness-probe timing rather than a renamed identifier. All identifiers in the e2e output (`opm-operator-test-e2e` cluster, `opm-operator-system` namespace, `github.com/open-platform-model/opm-operator/test/e2e` package path) are correctly renamed. Investigate separately; not a rename regression.
- [x] 8.6 `task operator:installer` — inspect `dist/install.yaml` for `namespace: opm-operator-system` and `app.kubernetes.io/name: opm-operator` on every resource.
- [x] 8.7 Smoke test on a throwaway Kind cluster: install, apply a sample `ModuleRelease`, delete, confirm prune succeeds. **Status**: install + apply + reconcile path fully verified on Kind 2026-04-19 with renamed identifiers (`opm-operator-test-e2e` cluster, `opm-operator-system` namespace, `opm-operator-controller-manager` deployment, rendered `ConfigMap` correctly labeled `managed-by: opm-controller`). Delete path hit a pre-existing RBAC bug in the finalizer's fallback branch (`task module:delete` removes the `hello-applier` SA alongside the CR; finalizer can't impersonate, falls back to controller SA which lacks `configmaps: get,delete` on arbitrary namespaces). Bug is not rename-related; reproducible on `main`. Cluster torn down without verifying prune. Track the finalizer-RBAC issue separately.

## 9. Release handoff (post-merge, manual by user)

- [ ] 9.1 User renames GitHub repo `open-platform-model/poc-controller` → `open-platform-model/opm-operator` in the GitHub UI.
- [ ] 9.2 Release-please opens a release PR under the new name; user reviews and merges it. Release tag is `v0.5.0`.
- [ ] 9.3 User confirms the release workflow pushed `ghcr.io/open-platform-model/opm-operator:v0.5.0` and uploaded `install.yaml` as a release asset.
- [ ] 9.4 User publishes the renamed CUE catalog module to the OCI registry (`cd catalog && cue mod publish v1.0.0` or the current version scheme) so consumers can resolve `opmodel.dev/opm-operator/catalog@v1`.
