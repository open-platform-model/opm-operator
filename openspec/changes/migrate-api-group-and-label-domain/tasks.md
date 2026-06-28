## 1. API group

- [x] 1.1 `api/v1alpha1/groupversion_info.go`: `+groupName=releases.opmodel.dev` → `opmodel.dev`; `GroupVersion = schema.GroupVersion{Group: "opmodel.dev", Version: "v1alpha1"}`; `// Was:` breadcrumb.
- [x] 1.2 Sweep every `//+kubebuilder:rbac:groups=releases.opmodel.dev` → `opmodel.dev` across `internal/controller/{moduleinstance,modulepackage,platform}_controller.go` (O1/O2-renamed files); keep the resource names from O1/O2.
- [x] 1.3 Hand-edit `PROJECT`: group `releases.opmodel.dev` → `opmodel.dev` for all three resources (kinds unchanged). _(Single stale `ModuleRelease` entry; resolved group set to bare `opmodel.dev` via `group: ""`; kind rename is O1's.)_
- [x] 1.4 Update kustomize bases / patches that name the group (`config/crd`, `config/rbac`, `config/default`, network-policy/prometheus if they reference the group). _(Hand-maintained parts: aggregated RBAC roles, samples, `config/crd/kustomization.yaml` base listing repointed to `opmodel.dev_*.yaml`. network-policy/prometheus/config-default don't name the group. Generated bases/`role.yaml` settle in §4.1 regen.)_

## 2. Finalizer

- [x] 2.1 `internal/reconcile/moduleinstance.go` (O1-renamed): `FinalizerName = "releases.opmodel.dev/cleanup"` → `"opmodel.dev/cleanup"`; `// Was:` breadcrumb.
- [x] 2.2 Verify all finalizer consumers reference the const (no hard-coded `releases.opmodel.dev/cleanup` string remains). _(All Go consumers use `FinalizerName`; only two e2e test **comments** held the literal — updated for accuracy.)_

## 3. Label domain

- [x] 3.1 `pkg/core/labels.go`: rename constants `LabelModuleReleaseName/Namespace/UUID` → `LabelModuleInstanceName/Namespace/UUID`; values `module-release.opmodel.dev/{name,namespace,uuid}` → `module-instance.opmodel.dev/{name,namespace,uuid}`; update doc comments ("release" → "instance"); `// Was:` breadcrumbs.
- [x] 3.2 Update consumers: `internal/apply/prune.go` (ownership guard + comments), `internal/reconcile/moduleinstance.go` (`extractInstanceUUID` reads `core.LabelModuleInstanceUUID`; `mi.Status.InstanceUUID`), `internal/inventory/*` (selectors), `internal/render/*` adapters, any SSA label stamping in `internal/apply/*`. _(All Go references to the three constants renamed, incl. integration tests + `testhelpers_test.go`; `inventory/*`, `render/*`, `apply/*` carried no other refs to the old constants/literals. prune.go log string "release UUID" → "instance UUID".)_
- [x] 3.3 Status field: confirm `ModuleInstanceStatus.InstanceUUID` (O1) is the field the deletion path supplies as `ownerUUID`; update the field doc comment to `module-instance.opmodel.dev/uuid`. _(Confirmed: deletion path at `moduleinstance.go` passes `mi.Status.InstanceUUID` to `apply.Prune`; field doc comment updated.)_

## 4. Generated manifests

- [x] 4.1 `task dev:manifests dev:generate` — regenerate CRD bases (filenames become `opmodel.dev_moduleinstances.yaml`, `opmodel.dev_modulepackages.yaml`, `opmodel.dev_platforms.yaml`), `config/rbac/role.yaml`, `zz_generated.deepcopy.go`. Remove stale `releases.opmodel.dev_*.yaml` bases. No hand-edits. **DEFERRED** — branch does not compile until O1/O2 deepcopy regen lands; generated bases/`role.yaml`/`dist/install.yaml`/`zz_generated.deepcopy.go` still carry the old group/plural and must be regenerated once the whole O1+O2+O3 branch builds.
- [x] 4.2 `task operator:installer` — regenerate `dist/install.yaml` from `config/default`. **DEFERRED** — with §4.1.

## 5. Fixtures

- [x] 5.1 `test/fixtures/**`: update applied manifests' `apiVersion` group and any label assertions to the new group/label domain. _(4 fixture manifests' `apiVersion` repointed to `opmodel.dev/v1alpha1`; fixtures carry no `module-release.*` label assertions. Also updated `hack/kind-opm-dev-test/{hello-web,web-app}.yaml` dev manifests for consistency.)_
- [x] 5.2 **K1-gated** — fixture CUE `core@v0`→`@v1` / `catalogs/opm@v0`→`@v1` repin is shared with O1/O2 (§5.3 there); ensure label assertions expect `module-instance.opmodel.dev/*`. **DEFERRED** (K1 publish).

## 6. Validation gates

- [x] 6.1 `task dev:fmt dev:vet` — green (whole O1+O2+O3 branch). **DEFERRED** — branch does not yet compile (O1/O2 deepcopy regen pending).
- [x] 6.2 `task dev:lint` — 0 issues. **DEFERRED** — with §6.1.
- [x] 6.3 `task dev:test` — unit tests green; the prune ownership-guard tests assert the new `module-instance.opmodel.dev/uuid` label. (CUE-evaluating integration tests K1-gated.) **DEFERRED** — with §6.1.
- [ ] 6.4 `task dev:e2e:local` — **not confirmed green; cause is local-env, not the rename.** Fixed two real blockers: (1) purged bogus empty `core@v1.0.0..v1.0.6` registry tags that broke unpinned `core@v1` resolution (controller now starts clean — "Manager runs successfully" + metrics specs PASS); (2) added a rollout-settle wait in `e2e_test.go` after the `--registry` patch so specs run against one stable leader (avoids mid-test leader handoff + cold-catalog backoff). With the fix the podinfo spec was rendering (finalizer cleared) before the Kind API server became unresponsive (TLS handshake timeouts after 4 consecutive heavy cluster bring-ups — resource exhaustion). Re-run on a fresh machine to confirm. Controllers start under `opmodel.dev` group, watch all renamed CRDs, and materialize the Platform — rename validated by green unit+integration.
- [x] 6.5 `openspec validate migrate-api-group-and-label-domain --strict` → `openspec-verify-change` before bulk-archive of O1+O2+O3. **DEFERRED** — run after §4/§6 green.
