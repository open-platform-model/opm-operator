## 1. Drift detection uses the apply identity

- [x] 1.1 `internal/status/conditions.go`: add the reason `DriftCheckForbidden` and `MarkDriftUnknown` (sets `Drifted=Unknown`, touches no other condition); verify with a unit test in `internal/status`
- [x] 1.2 `test/integration/reconcile/drift_identity_test.go`: envtest specs, written first and seen to fail on the unchanged reconcile: the dry-run `PATCH` carries `Impersonate-User` for `spec.serviceAccountName` and for `--default-service-account`; a ServiceAccount without `patch` gives `Drifted=Unknown`/`DriftCheckForbidden` with `Ready=True`, and the verdict returns with the verb; a deleted ServiceAccount gives `Drifted=Unknown`/`ImpersonationFailed` and no dry-run
- [x] 1.3 `internal/reconcile/moduleinstance.go`: build the apply client before drift detection, pass its resource manager and its error to `detectDrift`, reuse the client for the `NoOp` health reads, the apply and the prune; verify the specs of 1.2 pass and the existing drift, restore, impersonation and health specs still pass
- [x] 1.4 `internal/apply/drift_identity_test.go`: a unit test against an `httptest` API server proves that `DetectDrift` through `NewImpersonatedClient` sends `Impersonate-User` on the dry-run and returns an error that unwraps to `Forbidden` when refused
- [x] 1.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(controller): detect drift as the identity that applies`

## 2. Docs

- [x] 2.1 `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`, `docs/site/start/install-the-operator.md` and `adr/012-drift-detection-only.md`: state which identity runs drift detection and list `Drifted=Unknown` with its two reasons; verify with `grep -n DriftCheckForbidden` over the files and `task docs:bundle:check`
- [x] 2.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: state which identity runs drift detection`

## 3. Review fixes

- [x] 3.1 `internal/apply/drift.go`: read each object through the client of the dry-run before `Diff` and return a `Forbidden` read as an error; verify with an envtest spec for a ServiceAccount with `patch` and without `get` (`Drifted=Unknown`/`DriftCheckForbidden`, never `True`) that fails without the read
- [x] 3.2 `adr/012-drift-detection-only.md`: take the added sentences out of the Decision section and add a "See also" line under Status; correct the permission list (`get`, `create`, `patch`) on the three doc pages; name the real file and function in the change artifacts; verify with `openspec validate detect-drift-as-instance-identity --strict` and `task docs:bundle:check`
- [x] 3.3 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `fix(apply): report a refused read in drift detection as a refused drift check`
