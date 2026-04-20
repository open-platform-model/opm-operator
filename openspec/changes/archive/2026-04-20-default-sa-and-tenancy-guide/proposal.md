## Why

The controller already supports ServiceAccount impersonation via `spec.serviceAccountName`, but when the field is empty it falls back to the controller's own identity. The controller's RBAC is intentionally narrow, so the fallback produces an opaque apply failure rather than a clear authorization error attributable to a named SA. Flux solves the same problem with a `--default-service-account` manager flag: empty SA references resolve to a named SA in the release's namespace. Adopting that flag gives cleaner failure modes, better audit trails, and matches the multi-tenancy posture Flux users already understand.

Separately, opm-operator documents the mechanism (the `serviceaccount-impersonation` spec) but ships no tenancy convention. Platform admins have no guidance on the per-tenant-namespace SA pattern that Flux documents in `flux2-multi-tenancy`. Shipping a tenancy guide closes that gap without changing any behavior.

## What Changes

- Add `--default-service-account` flag to the manager binary (`cmd/main.go`). Default value: empty (preserves current behavior).
- When the flag is non-empty and a `ModuleRelease` or `BundleRelease` has `spec.serviceAccountName` empty, the controller resolves the impersonated identity to `system:serviceaccount:<releaseNamespace>:<flag-value>` instead of falling back to the controller's own client.
- When the flag is empty and `spec.serviceAccountName` is empty, existing behavior is preserved (controller identity, narrow RBAC, apply fails with forbidden).
- Add a multi-tenancy guide at `docs/TENANCY.md` documenting the per-namespace SA pattern: one SA per tenant namespace, one RoleBinding, N releases reference it. Include a worked example and a security note covering the escalation gadget from `docs/design/impersonation-and-privilege-escalation.md`.
- Update `docs/design/README.md` and link the tenancy guide from `CLAUDE.md` entrypoint docs.

Not in scope:
- No change to `impersonate` RBAC on the controller (stays narrow: `serviceaccounts` only).
- No deny-list for privileged SAs (deferred; scope-§9).
- No capability declarations or `opm-rbac-manager` sibling (deferred; scope-§9).

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `serviceaccount-impersonation`: add requirements governing the `--default-service-account` flag — how it resolves empty `spec.serviceAccountName`, how it interacts with missing-SA stall, and precedence vs explicit `spec.serviceAccountName`.

## Impact

- **Code**: `cmd/main.go` (flag registration + plumbing), `internal/controller/modulerelease_controller.go` and `internal/controller/release_controller.go` (resolve empty SA to flag default), potentially `internal/apply/impersonate.go` (no signature change expected; resolution happens at the caller).
- **Tests**: `internal/controller/*_test.go` for flag-resolution logic; `test/integration/reconcile/impersonation_test.go` for end-to-end behavior with and without the flag.
- **Docs**: new `docs/TENANCY.md`; updates to `docs/design/README.md`, `CLAUDE.md` entrypoint map.
- **Spec**: delta to `openspec/specs/serviceaccount-impersonation/spec.md`.
- **APIs**: no CRD schema change. No breaking change.
- **SemVer**: MINOR (additive flag, additive doc).
- **Dependencies**: none.
