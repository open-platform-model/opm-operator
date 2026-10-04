## 1. Gate check and spike

- [ ] 1.1 Check the gate, and stop and report to the supervisor if it does not hold: the catalog_opm change `add-seccomp-and-subjectless-roles` is merged, and a catalog release `opmodel.dev/catalogs/opm` v4 carrying it is published on GHCR (`cue mod get opmodel.dev/catalogs/opm@v4.<x>.<y>` resolves it); that release's seccomp fields (pod and container level) and its subject-less `#Role` render as 0028:D12:R1/R2 state; and the core `v2` version it requires renders under the operator's pinned library (`go.mod`). Record the release version and the exact field names in design.md, "Catalog surfaces of 0028:D12"
- [ ] 1.2 Spike: copy `enhancements/0028/experiments/01-operator-module-render/module` into a scratch directory, rewrite its path to `opmodel.dev/modules/opm_operator@v0`, pin the gate's catalog release, and render it from Go with library `Kernel.AcquireModuleFromDir`, `SynthesizeInstance` and `Render` against a platform from `opm/helper/platformmodule` generated from the module's own catalog pin; record whether an unpublished module renders this way, or that the render test must drive the pinned `opm module build` instead
- [ ] 1.3 Spike: in the same scratch module, constrain `#ctx.instance` to `opm-operator` / `opm-operator-system` and render with another name; record whether the kernel's error names the expected coordinates, or which guard the module uses instead
- [ ] 1.4 Spike: in an envtest API server, create a Pod in a namespace labeled `pod-security.kubernetes.io/enforce: restricted` with and without `seccompProfile`; record whether the API server enforces Pod Security, or that the test uses `k8s.io/pod-security-admission/policy` instead
- [ ] 1.5 Write the three findings into design.md under "Research & Decisions", adjusting the render-test and guard decisions if a fallback was taken; run `openspec validate add-operator-module --strict`
- [ ] 1.6 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(openspec): record the add-operator-module gate and spike findings`

## 2. Generated CRD and RBAC data with a drift check

- [ ] 2.1 Create `modules/opm_operator/cue.mod/module.cue` (`opmodel.dev/modules/opm_operator@v0`, language `v0.17.0`, source `self`) and pin core v2 and the gate's catalog release with `cue mod get`, then `cue mod tidy`; never hand-edit the pins
- [ ] 2.2 Add `hack/operator-module/generate.sh` from the experiment's `hack/generate.sh`: CRDs from `config/crd/bases/*.yaml` into `#crdSource`, and the eight role files of `config/rbac` (`role.yaml`, `leader_election_role.yaml`, `metrics_auth_role.yaml`, `metrics_reader_role.yaml`, `moduleinstance_{admin,editor,viewer}_role.yaml`, `transformerregistration_admin_role.yaml`) into `#rbacSource`, keyed by unprefixed name, with a `DO NOT EDIT` header; generate `modules/opm_operator/zz_generated_crds.cue` and `zz_generated_rbac.cue`
- [ ] 2.3 Add `hack/operator-module/drift-check.sh`: regenerate into a temp dir and diff the bodies against the committed files, naming each stale file; `--ref <tag>` extracts `config/` from that tag with `git archive` and checks against it instead
- [ ] 2.4 Add `.tasks/operator-module.yaml` (included as `operator-module`) with `generate` and `drift`; `drift` first runs `:dev:manifests` and fails when `config/crd/bases` or `config/rbac/role.yaml` then differ from the index
- [ ] 2.5 Add a `test.yml` step after `Setup CUE` that runs `task operator-module:drift`; check it fails on a scratch copy with one RBAC verb and one CRD short name added, and passes on the tree and with `--ref` at the latest operator release tag
- [ ] 2.6 `task dev:fmt dev:vet dev:lint dev:test` and `task operator-module:drift` green, then commit `build(module): generate the operator module's CRD and RBAC data from config`

## 3. The module through the catalog, with its render test

- [ ] 3.1 Add `modules/opm_operator/identity/identity.cue` (`ModulePath`, `Version: "0.1.0"`) per core `#IdentityPackage`
- [ ] 3.2 Add `modules/opm_operator/operator/operator.cue` with `Version` and `Image {repository, tag, digest}` of the latest published operator release, the tag derived from `Version` and the digest read from GHCR for that tag (0028:D1:R11)
- [ ] 3.3 Add `module.cue`: metadata from the identity package, the instance-coordinate guard from 1.3, `#config` (image repository, `registry?`, `defaultServiceAccount?`, `resources`, `replicas`, `extraArgs` refusing the typed and module-owned flags), `debugValues: {}`, and the derived `GOMEMLIMIT` (80 percent of the memory limit in MiB, `Mi` and `Gi` accepted) (0028:D5:R1/R4/R6)
- [ ] 3.4 Add `components.cue`: Namespace, CRDs (each imported `spec` embedded whole), the `controller-manager` workload with ServiceAccount, Service, `control-plane: controller-manager` label and the pod and container seccomp profile, three bound `#Role` components and five subject-less `#Role` components from `#rbacSource`, every name a constant (0028:D2:R8/R10/R12)
- [ ] 3.5 Add `test/integration/operatormodule` with the render path chosen in 1.2, skipping without a registry and failing under `OPM_TEST_REGISTRY_FORCE=1`; assert the 19 objects and their names, the literal Deployment selector, no raw-objects component, CRD `spec` equality with `config/crd/bases`, role rules equal to `config/rbac`, and the default image `<repository>:v<Version>@<digest>` (0028:D2:R1/R2/R10/R11/R12, 0028:D1:R11)
- [ ] 3.6 `task dev:fmt dev:vet dev:lint dev:test` and `task operator-module:drift` green, then commit `build(module): render the operator through the catalog as an OPM module`

## 4. Tuning surface and Pod Security coverage

- [ ] 4.1 Render test: every `#config` value set (repository, registry mapping, default service account, resources, two replicas, two extra arguments) reaches the Deployment in the documented argument order, and `GOMEMLIMIT` follows the memory limit (`4Gi` gives `3276MiB`) (0028:D5:R1)
- [ ] 4.2 Render test: a mirrored repository keeps the module's tag and digest (0028:D5:R5); every value set leaves the selector equal to the literal (0028:D2:R11)
- [ ] 4.3 Render test refusals: an image tag value, an image digest value, `--registry=` and `--default-service-account=` in `extraArgs`, a memory limit in an unsupported unit, and an instance other than `opm-operator` in `opm-operator-system`, each failing with an error that names the field or the expected value (0028:D5:R4/R6, 0028:D3:R18)
- [ ] 4.4 Pod Security test, by the mechanism chosen in 1.4: a Pod from the rendered template is admitted under `restricted`, at defaults and with every value set (0028:D2:R8)
- [ ] 4.5 `task dev:fmt dev:vet dev:lint dev:test` and `task operator-module:drift` green, then commit `test(module): cover the operator module's tuning surface and Pod Security posture`

## 5. Documentation

- [ ] 5.1 Add `modules/opm_operator/README.md`: what the module renders, `#config` with each field's effect, that the image tag and digest come from `operator/operator.cue`, how to regenerate (`task dev:manifests operator-module:generate`), and that it is not published by this repository yet
- [ ] 5.2 Update AGENTS.md: the layout entry for `modules/`, the two generated files under "Generated Files And Scaffold Boundaries", the `operator-module:*` tasks, and the rule that an API or RBAC marker change runs `task operator-module:generate` with `dev:manifests`
- [ ] 5.3 Name the `test/integration/operatormodule` package and its registry requirement in AGENTS.md "Testing Style" (the `docs/TESTING.md` that section links does not exist in the tree; do not create it here)
- [ ] 5.4 `task dev:fmt dev:vet dev:lint dev:test`, `task operator-module:drift` and `task docs:bundle:check` green, then commit `docs(module): document the operator module and its regeneration`
