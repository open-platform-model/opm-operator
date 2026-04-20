## 1. Flag registration and plumbing

- [x] 1.1 Add `--default-service-account` string flag to `cmd/main.go` (empty default, help text mentions per-release-namespace semantics and cross-links tenancy guide)
- [x] 1.2 Add `DefaultServiceAccount string` field to `internal/reconcile/modulerelease.go` `ModuleReleaseParams` (and the equivalent params struct used by `release_controller.go`)
- [x] 1.3 Wire the flag value from `main.go` through controller construction into each controller's params struct
- [x] 1.4 `task dev:fmt dev:vet` clean

## 2. Resolution logic

- [x] 2.1 In `buildApplyClient` (both `modulerelease.go` and `release.go`), resolve `effectiveSA := spec.ServiceAccountName; if effectiveSA == "" { effectiveSA = params.DefaultServiceAccount }` before the empty-check
- [x] 2.2 Preserve existing "both empty → controller's own client" path unchanged
- [x] 2.3 Log the effective SA name and whether it was resolved from spec or flag default (structured log key `serviceAccount`, new key `serviceAccountSource=spec|default`)
- [x] 2.4 Confirm `ImpersonationFailed` stall reason already covers flag-defaulted missing SA; no new condition reason added

## 3. Unit tests (reconcile package)

- [x] 3.1 Add test: empty spec + empty flag → no impersonation (controller client returned)
- [x] 3.2 Add test: empty spec + non-empty flag + SA exists in release namespace → impersonated client built for `system:serviceaccount:<releaseNs>:<flag>`
- [x] 3.3 Add test: empty spec + non-empty flag + SA missing in release namespace → error stalls with `ImpersonationFailed`
- [x] 3.4 Add test: non-empty spec + non-empty flag → explicit spec wins, flag ignored
- [x] 3.5 Add test: flag names an SA that exists only in controller namespace but not release namespace → stalls (no cross-namespace fallback)

## 4. Integration test

- [x] 4.1 Extend `test/integration/reconcile/impersonation_test.go` with a case starting the manager with `--default-service-account=opm-deployer`, creating the SA + RoleBinding in the tenant namespace, and applying a ModuleRelease with empty `spec.serviceAccountName`; assert apply succeeds under the defaulted identity
- [x] 4.2 Add case: manager started with `--default-service-account=missing`, release with empty spec, SA absent → release stalls with `ImpersonationFailed`

## 5. Documentation

- [x] 5.1 Create `docs/TENANCY.md` with: intro, recommended per-namespace SA pattern, worked example (SA + RoleBinding + two ModuleReleases sharing the SA), `--default-service-account` lockdown section, security note linking `docs/design/impersonation-and-privilege-escalation.md`
- [x] 5.2 Update `docs/design/README.md` to link `docs/TENANCY.md` (top-level docs are referenced from design README for cross-discovery)
- [x] 5.3 Update `CLAUDE.md` entrypoint section to list `docs/TENANCY.md` alongside `docs/STYLE.md` and `docs/TESTING.md`
- [x] 5.4 Add a brief note in `docs/design/rbac-delegation-crossplane-flux.md` §"Proposed direction" confirming the flag landed

## 6. Validation gates

- [x] 6.1 `task dev:fmt dev:vet`
- [x] 6.2 `task dev:lint`
- [x] 6.3 `task dev:test`
- [x] 6.4 Confirm no generated-file churn (no `task dev:manifests dev:generate` needed — no CRD/API changes)

## 7. Deletion cleanup parity (post-verify follow-up)

- [x] 7.1 Extend `handleDeletion` in `internal/reconcile/modulerelease.go` to resolve the effective SA via `resolveEffectiveSA` (spec > flag) before attempting impersonation; preserve best-effort fallback to controller client on impersonation failure so finalizers never block
- [x] 7.2 Mirror the change in `handleReleaseDeletion` in `internal/reconcile/release.go`
- [x] 7.3 Add `serviceAccountSource` to the fallback log alongside `serviceAccount` so operators can see whether the missing SA was spec-provided or flag-defaulted
- [x] 7.4 Add a unit test in `internal/reconcile/default_sa_test.go` covering flag-only resolution for the deletion path (empty spec + non-empty flag → effective SA is the flag value)
- [x] 7.5 Extend `docs/TENANCY.md` §"Lockdown: `--default-service-account`" with a one-paragraph note that deletion cleanup follows the same resolution and is best-effort
- [x] 7.6 Update the `serviceaccount-impersonation` delta spec with a "Flag default applies to deletion cleanup" requirement + two scenarios (SA present, SA missing)
- [x] 7.7 `task dev:fmt dev:vet dev:lint dev:test`
