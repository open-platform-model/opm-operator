## 1. Drift detection uses the apply identity

- [ ] 1.1 `internal/status/conditions.go`: add the reason `DriftCheckForbidden` and `MarkDriftUnknown` (sets `Drifted=Unknown`, touches no other condition); verify with a unit test in `internal/status`
- [ ] 1.2 `test/integration/reconcile/drift_identity_test.go`: envtest specs, written first and seen to fail on the unchanged reconcile: the dry-run `PATCH` carries `Impersonate-User` for `spec.serviceAccountName` and for `--default-service-account`; a ServiceAccount without `patch` gives `Drifted=Unknown`/`DriftCheckForbidden` with `Ready=True`, and the verdict returns with the verb; a deleted ServiceAccount gives `Drifted=Unknown`/`ImpersonationFailed` and no dry-run
- [ ] 1.3 `internal/reconcile/moduleinstance.go`: build the apply client before drift detection, pass its resource manager and its error to `detectDrift`, reuse the client for the `NoOp` health reads, the apply and the prune; verify the specs of 1.2 pass and the existing drift, restore, impersonation and health specs still pass
- [ ] 1.4 `internal/apply/drift_identity_test.go`: a unit test against an `httptest` API server proves that `DetectDrift` through `NewImpersonatedClient` sends `Impersonate-User` on the dry-run and returns an error that unwraps to `Forbidden` when refused
- [ ] 1.5 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(controller): detect drift as the identity that applies`

## 2. Docs

- [ ] 2.1 `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`, the tenancy page and `adr/012-drift-detection-only.md`: state which identity runs drift detection and list `Drifted=Unknown` with its two reasons; verify with `grep -n DriftCheckForbidden` over the files and `task docs:bundle:check`
- [ ] 2.2 `task dev:fmt dev:vet dev:lint dev:test docs:bundle:check` green, then commit `docs: state which identity runs drift detection`
