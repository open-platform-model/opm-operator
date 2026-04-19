## 1. Flag registration and plumbing

- [ ] 1.1 Add `--default-service-account` string flag to `cmd/main.go` (empty default, help text mentions per-release-namespace semantics and cross-links tenancy guide)
- [ ] 1.2 Add `DefaultServiceAccount string` field to `internal/reconcile/modulerelease.go` `ModuleReleaseParams` (and the equivalent params struct used by `release_controller.go`)
- [ ] 1.3 Wire the flag value from `main.go` through controller construction into each controller's params struct
- [ ] 1.4 `task dev:fmt dev:vet` clean

## 2. Resolution logic

- [ ] 2.1 In `buildApplyClient` (both `modulerelease.go` and `release.go`), resolve `effectiveSA := spec.ServiceAccountName; if effectiveSA == "" { effectiveSA = params.DefaultServiceAccount }` before the empty-check
- [ ] 2.2 Preserve existing "both empty → controller's own client" path unchanged
- [ ] 2.3 Log the effective SA name and whether it was resolved from spec or flag default (structured log key `serviceAccount`, new key `serviceAccountSource=spec|default`)
- [ ] 2.4 Confirm `ImpersonationFailed` stall reason already covers flag-defaulted missing SA; no new condition reason added

## 3. Unit tests (reconcile package)

- [ ] 3.1 Add test: empty spec + empty flag → no impersonation (controller client returned)
- [ ] 3.2 Add test: empty spec + non-empty flag + SA exists in release namespace → impersonated client built for `system:serviceaccount:<releaseNs>:<flag>`
- [ ] 3.3 Add test: empty spec + non-empty flag + SA missing in release namespace → error stalls with `ImpersonationFailed`
- [ ] 3.4 Add test: non-empty spec + non-empty flag → explicit spec wins, flag ignored
- [ ] 3.5 Add test: flag names an SA that exists only in controller namespace but not release namespace → stalls (no cross-namespace fallback)

## 4. Integration test

- [ ] 4.1 Extend `test/integration/reconcile/impersonation_test.go` with a case starting the manager with `--default-service-account=opm-deployer`, creating the SA + RoleBinding in the tenant namespace, and applying a ModuleRelease with empty `spec.serviceAccountName`; assert apply succeeds under the defaulted identity
- [ ] 4.2 Add case: manager started with `--default-service-account=missing`, release with empty spec, SA absent → release stalls with `ImpersonationFailed`

## 5. Documentation

- [ ] 5.1 Create `docs/TENANCY.md` with: intro, recommended per-namespace SA pattern, worked example (SA + RoleBinding + two ModuleReleases sharing the SA), `--default-service-account` lockdown section, security note linking `docs/design/impersonation-and-privilege-escalation.md`
- [ ] 5.2 Update `docs/design/README.md` to link `docs/TENANCY.md` (top-level docs are referenced from design README for cross-discovery)
- [ ] 5.3 Update `CLAUDE.md` entrypoint section to list `docs/TENANCY.md` alongside `docs/STYLE.md` and `docs/TESTING.md`
- [ ] 5.4 Add a brief note in `docs/design/rbac-delegation-crossplane-flux.md` §"Proposed direction" confirming the flag landed

## 6. Validation gates

- [ ] 6.1 `task dev:fmt dev:vet`
- [ ] 6.2 `task dev:lint`
- [ ] 6.3 `task dev:test`
- [ ] 6.4 Confirm no generated-file churn (no `task dev:manifests dev:generate` needed — no CRD/API changes)
