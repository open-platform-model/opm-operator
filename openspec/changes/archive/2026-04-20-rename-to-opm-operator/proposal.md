## Why

ADR-014 explicitly split the rename of in-cluster, module, and repo identifiers off as follow-up work after the image rename to `ghcr.io/open-platform-model/opm-operator` landed. The published image now says `opm-operator`, the SSA field manager and managed-by label value are already `opm-controller`, but:

- The Go module path is still `github.com/open-platform-model/poc-controller`.
- The GitHub repo is still `open-platform-model/poc-controller`.
- The CUE catalog module path is still `opmodel.dev/poc-controller/catalog@v1`.
- Kustomize emits a `poc-controller-system` namespace, a `poc-controller-` name prefix, and `app.kubernetes.io/name: poc-controller` labels on every manager manifest.
- Build and task plumbing names (Kind cluster default, buildx builder, fixture paths) still embed `poc-controller`.

The divergence is confusing on every surface an operator or contributor sees: `kubectl get pods -n opm-operator-system` will fail today, the installable YAML pulls `opm-operator` but lives in `poc-controller-system`, and the Go import path does not match the published image. This change reconciles all of those to `opm-operator`.

## What Changes

- **Go module path**: `github.com/open-platform-model/poc-controller` → `github.com/open-platform-model/opm-operator`. All 153 import sites across 59 Go files updated in one mechanical pass. `go.mod`, `PROJECT` (`projectName` and `repo` fields), `release-please-config.json` (`package-name`).
- **CUE catalog module path**: `opmodel.dev/poc-controller/catalog@v1` → `opmodel.dev/opm-operator/catalog@v1` in `catalog/cue.mod/module.cue`. No workspace consumers found; safe to rename without a bridge.
- **In-cluster identity**: `config/default/kustomization.yaml` namespace `poc-controller-system` → `opm-operator-system`; `namePrefix: poc-controller-` → `opm-operator-`. All 14 manifests under `config/` that set `app.kubernetes.io/name: poc-controller` switch to `opm-operator`. Hardcoded constants in `test/e2e/e2e_test.go` (`namespace`, `serviceAccountName`, `metricsServiceName`, `metricsRoleBindingName`, `metricsRoleName`, and `--clusterrole=poc-controller-metrics-reader`) updated to match.
- **Build / CI glue**: `.tasks/docker.yaml` buildx builder name, `.tasks/release.yaml` OCI publish source paths, `.tasks/operator.yaml` and `.tasks/kind.yaml` kubectl commands referencing the old namespace and deployment name, `Taskfile.yml` `KIND_CLUSTER` default, `Makefile` equivalents.
- **Docs**: `README.md`, `CLAUDE.md`, `CONSTITUTION.md`, `docs/STYLE.md`, `docs/TESTING.md` prose updated. Active spec `openspec/specs/container-image-publish/spec.md` retains its `opm-operator` image references (unchanged). ADR-015 added to record the rename decision. ADR-014's "follow-up work" trade-off paragraph left intact as historical record.
- **Workspace coupling** (outside this repo, owned by the rename PR): root `CLAUDE.md` quick-map and directory map, `open-platform-model.code-workspace`, root `Taskfile.yml` `find catalog cli poc-controller modules releases`, `releases/Taskfile.yml` kubectl patch targets. Disk-level directory rename (`poc-controller/` → `opm-operator/`) is performed by the user outside the repo's own PR; this proposal assumes that directory name is the one in effect after the rename PR merges.
- **GitHub repo rename**: performed manually by the user between merge of the rename PR and merge of the release-please PR it triggers. GitHub preserves redirects for git operations.
- **Explicitly out of scope**: no change to the CRD `releases.opmodel.dev` API group, no change to any label or annotation that lives on user workloads (`app.kubernetes.io/managed-by: opm-controller`, `module-release.opmodel.dev/*`, `component.opmodel.dev/*`), no change to `FieldManager = "opm-controller"`, no change to the published image repository. User-applied `ModuleRelease` and `BundleRelease` resources are untouched.

## Capabilities

### Modified Capabilities

_None._ This change renames identifiers only; no controller behavior, CRD shape, emitted-label, or CI-published artifact is modified.

### New Capabilities

_None._

## Impact

- **Affected files**: every `.go` file in the module (imports), every manifest under `config/`, every file under `.tasks/`, `Taskfile.yml`, `Makefile`, `go.mod`, `PROJECT`, `release-please-config.json`, `catalog/cue.mod/module.cue`, `test/e2e/e2e_test.go`, `test/e2e/e2e_suite_test.go` (includes the `example.com/poc-controller:v0.0.1` test image placeholder), `README.md`, `CLAUDE.md`, `CONSTITUTION.md`, `docs/STYLE.md`, `docs/TESTING.md`. Sibling workspace files: `../CLAUDE.md`, `../open-platform-model.code-workspace`, `../Taskfile.yml`, `../releases/Taskfile.yml`.
- **Explicitly excluded from editing**: `CHANGELOG.md` (historical release-please output; release-please will emit new entries under the new repo name going forward), everything under `openspec/changes/archive/` (immutable history), git tags `v0.4.x` (immutable).
- **External dependencies**: none added or removed.
- **GitHub settings**: user renames repo manually; GitHub provides automatic git redirects. No change to workflow permissions or package settings.
- **Consumer impact**:
  - *Go consumers*: no workspace consumers detected; no external consumers known. Old module path continues to resolve via Go proxy for historical tags; the old path is simply not advanced. No bridging `retract` directive or final `v0.4.5` release is published under the old name — the cost/benefit does not justify it.
  - *CUE consumers of the catalog module*: no workspace consumers detected; same posture as Go consumers.
  - *Cluster operators*: in-place upgrade of a running `v0.4.x` controller to `v0.5.0` is not possible because the Deployment's `matchLabels` selector is immutable and `app.kubernetes.io/name` is part of it. The documented upgrade procedure is uninstall-then-install: `kubectl delete -n poc-controller-system` of the manager deployment and associated RBAC, then apply the new release's `install.yaml`. CRDs are cluster-scoped and unaffected. User-created `ModuleRelease` and `BundleRelease` resources live in user namespaces and carry `app.kubernetes.io/managed-by: opm-controller` — they remain claimed by the renamed controller without edits.
- **SemVer classification**: MINOR — `0.4.4 → 0.5.0`. Pre-1.0 semantics allow breaking changes at any minor bump; the minor (not patch) bump signals to operators that an upgrade is required. Go semver does not force a `/v2` suffix because we remain on `v0`.
