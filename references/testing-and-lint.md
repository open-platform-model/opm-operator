# Tests, lint and the linter version

Covers the three test tiers, how to run one test, what the lint config enforces, and how to move the golangci-lint version. Fixtures and registries are not covered here.

## Contents

- Test tiers
- Run one test
- Lint expectations
- Moving the golangci-lint version
- Pitfalls
- Check before you finish

## Test tiers

Pick the lightest tier that proves the behaviour.

| Tier | Where | What it proves | Runs in |
| --- | --- | --- | --- |
| Unit | `internal/*/` | Single-package logic | `task dev:test` |
| Integration | `test/integration/` (`apply`, `crdvalidation`, `operatormodule`, `reconcile`) | Cross-package behaviour against a real API server (envtest) | `task dev:test` |
| E2E | `test/e2e/` (`e2e` build tag) | The deployed controller on a Kind cluster | `task dev:e2e` |

- `task dev:test` runs unit and integration tests and excludes `/test/e2e`. No Kind test runs in the default path.
- E2E needs Kind. It may install cert-manager unless `CERT_MANAGER_INSTALL_SKIP=true`. Say so in your report when you skipped e2e for lack of Kind or a cluster.
- Style: Ginkgo v2 and Gomega. Give `Describe`, `Context` and `It` descriptive text so `-ginkgo.focus` works. Use `Eventually` for async Kubernetes behaviour, never sleeps. Assert errors with `Expect(err).NotTo(HaveOccurred())` or `Expect(...).To(Succeed())`. Put repeated assertions in package-local helpers.
- `test/integration/operatormodule` renders the operator module (`modules/opm_operator`) against a platform generated from its own catalog pin. It needs core and the catalog from `CUE_REGISTRY` (GHCR under `task dev:test`), the `cue` CLI and the kustomize binary that `task dev:test` passes as `KUSTOMIZE`. Without them its specs skip; under `OPM_TEST_REGISTRY_FORCE=1` (PR CI) they fail. Its minimum-operator-version spec needs none of them.
- `test/integration/crdvalidation` proves that every sample in `config/samples` is admitted by the CRDs.

## Run one test

There is no single-test task; use `go test`. The envtest binaries are installed as a dependency of `task dev:test`: run it once to provision them. The Kubernetes version is derived from `k8s.io/api` in `go.mod` by `ENVTEST_K8S_VERSION` in `Taskfile.yml`.

```bash
export KUBEBUILDER_ASSETS="$(./bin/setup-envtest use <k8s-minor> --bin-dir "$PWD/bin" -p path)"
go test ./internal/controller                                   # one package
go test ./internal/controller -run <suite-func>                 # one suite entrypoint
go test ./internal/controller -run <suite-func> -ginkgo.focus="ModuleInstance Controller"
go test -tags=e2e ./test/e2e -v -ginkgo.v                       # e2e only
KIND_CLUSTER=opm-operator-test-e2e go test -tags=e2e ./test/e2e -run <suite-func> -ginkgo.focus="should run successfully" -v -ginkgo.v
```

`<k8s-minor>` is the `1.N` that `task dev:test` prints in its envtest line. `KUBEBUILDER_ASSETS` must be an absolute path.

## Lint expectations

- `.golangci.yml` enables, among others: `errcheck`, `ginkgolinter`, `gocyclo`, `govet`, `misspell`, `modernize`, `revive`, `staticcheck`, `unused`, `depguard`, `logcheck`. Formatters: `gofmt` and `goimports`.
- `logcheck` (a custom plugin built from the custom-gcl config) validates structured logging and balanced key/value parameters.
- `lll` and `dupl` are relaxed in `api/*` and `internal/*`. Do not rely on the exclusions without need.
- The `depguard` rule keeps the operator from copying the library's Kubernetes tier decisions (the comment in `.golangci.yml` says why).
- `ginkgolinter` flags non-idiomatic Ginkgo.

## Moving the golangci-lint version

`.golangci-lint-version` is the only place that names the linter version. `Taskfile.yml` and `Makefile` read it. The custom-gcl config at the repo root repeats it because `golangci-lint custom` reads only that file; `task dev:lint:config` refuses a tree where the two differ.

No workflow, task file or `Makefile` line runs `golangci-lint config verify` on its own. Without `--schema` it downloads the schema from golangci-lint.run on every run, which failed the cli's required `Lint` job twice on 2026-10-08.

- **Patch move** (`v2.8.0` to `v2.8.1`): edit `.golangci-lint-version` and the `version:` line of the custom-gcl config. The schema is per minor line and stays.
- **Minor or major move**: edit both files, then replace the schema. Take `jsonschema/golangci.next.jsonschema.json` of the Go module `github.com/golangci/golangci-lint/v2` at the new version; the Go checksum database verifies it. Do not take `jsonschema/golangci.jsonschema.json`: the linter project updates that file only after a release, so at a release tag it still holds the line before.
  1. `dir=$(go mod download -json github.com/golangci/golangci-lint/v2@vX.Y.Z | jq -r .Dir)`
  2. Copy `$dir/jsonschema/golangci.next.jsonschema.json` to `.github/golangci-lint/golangci.vX.Y.jsonschema.json` and `chmod 644` it.
  3. Remove the old schema file. In that directory run `sha256sum golangci.vX.Y.jsonschema.json > SHA256SUMS`.
  4. Say in the PR body where the file came from, so the reviewer can compare the checksum.
- `task dev:lint:config dev:lint:config:test dev:lint` must pass.
- The scenario `value-of-this-line` in the lint-config test in `.github/scripts/` proves the schema is not the one of the line before. On a minor move, change it to a value the new line added.
- The check uses the hidden `--schema` flag of `golangci-lint config verify`. If a release drops the flag, the check fails on the bump PR and needs a new design, never a dropped check.
- Installing the linter needs the network (the Go module proxy and the plugin build), and `task dev:lint*` reinstalls it on every run. Only the configuration check is offline: with the linter in `bin/`, `GOLANGCI_LINT=bin/golangci-lint bash .github/scripts/lint-config-check.sh` runs without a network.

## Pitfalls

- A relative `KUBEBUILDER_ASSETS` makes envtest suites fail to find the binaries.
- A schema taken from `golangci.jsonschema.json` is one release behind.
- Editing `.golangci-lint-version` without the custom-gcl config fails `task dev:lint:config`.
- Adding a bare `golangci-lint config verify` to CI reintroduces the schema download.
- Registry-backed specs skip silently without `CUE_REGISTRY` outside PR CI; check the skip count.

## Check before you finish

- [ ] `task dev:fmt dev:vet dev:test` pass after a Go change.
- [ ] `task dev:lint` passes for a non-trivial change.
- [ ] After a linter move, `task dev:lint:config dev:lint:config:test dev:lint` pass.
- [ ] You said whether e2e ran or was skipped.
