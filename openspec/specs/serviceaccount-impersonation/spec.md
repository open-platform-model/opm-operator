## ADDED Requirements

### Requirement: Impersonated client for apply and prune
When `spec.serviceAccountName` is set, the controller MUST use an impersonated client for apply (Phase 5) and prune (Phase 6) operations.

#### Scenario: Apply with impersonation
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=deploy-sa` in namespace `team-a`
- **WHEN** the controller runs Phase 5 (Apply)
- **THEN** SSA apply operations use the identity `system:serviceaccount:team-a:deploy-sa`
- **AND** the apply succeeds only if `deploy-sa` has sufficient RBAC permissions

#### Scenario: Prune with impersonation
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=deploy-sa` and stale resources to prune
- **WHEN** the controller runs Phase 6 (Prune)
- **THEN** delete operations use the impersonated identity

### Requirement: Default behavior without serviceAccountName
When `spec.serviceAccountName` is empty, the effective impersonation target MUST be determined by the manager's `--default-service-account` flag:

- If the flag is empty (the default), the controller MUST use its own client (existing behavior).
- If the flag is non-empty, the controller MUST build an impersonated client as if `spec.serviceAccountName` had been set to the flag value, resolving the ServiceAccount in the **release's** namespace (not the controller's namespace).

#### Scenario: No impersonation when flag and spec both empty
- **GIVEN** a ModuleRelease with `spec.serviceAccountName` empty or unset
- **AND** the manager started without `--default-service-account` (or with it empty)
- **WHEN** the controller reconciles
- **THEN** all apply and prune operations use the controller's own service account
- **AND** no impersonation headers are sent

#### Scenario: Flag-defaulted impersonation when spec empty
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName` empty
- **AND** the manager started with `--default-service-account=opm-deployer`
- **AND** a ServiceAccount `opm-deployer` exists in namespace `team-a`
- **WHEN** the controller reconciles
- **THEN** apply and prune operations impersonate `system:serviceaccount:team-a:opm-deployer`
- **AND** the impersonation config carries the standard SA groups (`system:serviceaccounts`, `system:serviceaccounts:team-a`, `system:authenticated`)

### Requirement: Missing ServiceAccount stalls reconcile
If the specified ServiceAccount does not exist, the reconcile MUST fail with a stalled condition.

#### Scenario: ServiceAccount not found
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=nonexistent-sa`
- **WHEN** the controller attempts to build an impersonated client
- **THEN** the reconcile is classified as `FailedStalled`
- **AND** `Ready=False` with reason indicating SA not found

### Requirement: Impersonation RBAC failure stalls reconcile
If the controller lacks impersonation permissions, the reconcile MUST fail with a stalled condition.

#### Scenario: Impersonation unauthorized
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=deploy-sa` and the controller lacking `impersonate` RBAC
- **WHEN** the controller attempts an apply operation
- **THEN** the reconcile is classified as `FailedStalled`
- **AND** the error message indicates impersonation was denied

### Requirement: Same-namespace only
The ServiceAccount MUST be in the same namespace as the ModuleRelease.

#### Scenario: SA resolved in same namespace
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName=deploy-sa`
- **WHEN** the controller builds the impersonated client
- **THEN** it impersonates `system:serviceaccount:team-a:deploy-sa` (same namespace)

### Requirement: Impersonation includes standard SA group set
The impersonated client MUST be configured with both `UserName` and `Groups` on the `rest.ImpersonationConfig`. The Kubernetes apiserver does not derive group membership from the impersonated `UserName`; it reads `Impersonate-Group` headers independently. Without explicit groups, the impersonated identity belongs to no groups, and any RBAC binding whose subject targets a group (`system:serviceaccounts`, `system:serviceaccounts:<namespace>`, or `system:authenticated`) silently fails — even though the same SA succeeds when authenticating with its own token.

The `Groups` slice MUST contain the standard set that the apiserver's `serviceaccount.TokenAuthenticator` would inject for an SA in the given namespace:

- `system:serviceaccounts`
- `system:serviceaccounts:<namespace>` (where `<namespace>` is the SA's namespace)
- `system:authenticated`

This matches the behavior of Flux's `runtime/client/impersonation` and gives impersonated identity parity with token-based identity for the same SA.

#### Scenario: Impersonation config carries standard groups
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName=deploy-sa`
- **WHEN** the controller builds the impersonated client
- **THEN** the underlying `rest.ImpersonationConfig.Groups` is exactly `["system:serviceaccounts", "system:serviceaccounts:team-a", "system:authenticated"]`
- **AND** `rest.ImpersonationConfig.UserName` is `system:serviceaccount:team-a:deploy-sa`

#### Scenario: Group-subject RoleBinding authorizes apply
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName=deploy-sa` and a RoleBinding in `team-a` whose subjects are `[{Kind: "Group", Name: "system:serviceaccounts:team-a"}]` granting permissions on the resources to be applied
- **WHEN** the controller runs Phase 5 (Apply)
- **THEN** the apply succeeds (the impersonated identity is recognized as a member of `system:serviceaccounts:team-a`)
- **AND** `Ready=True` is set on the ModuleRelease

#### Scenario: Authenticated-group binding authorizes read access
- **GIVEN** a ClusterRoleBinding granting `view` on a CRD to the group `system:authenticated`
- **WHEN** the controller's impersonated client lists instances of that CRD
- **THEN** the request is authorized (the impersonated identity is a member of `system:authenticated`)

### Requirement: Explicit spec.serviceAccountName takes precedence over flag default
When `spec.serviceAccountName` is non-empty, the controller MUST impersonate the named ServiceAccount regardless of the `--default-service-account` flag value. The flag is only a fallback for empty `spec.serviceAccountName`.

#### Scenario: Explicit SA overrides flag default
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName=custom-sa`
- **AND** the manager started with `--default-service-account=opm-deployer`
- **AND** both `custom-sa` and `opm-deployer` exist in namespace `team-a`
- **WHEN** the controller reconciles
- **THEN** apply and prune impersonate `system:serviceaccount:team-a:custom-sa`
- **AND** `opm-deployer` is not referenced by any API call on behalf of this release

### Requirement: Flag-defaulted SA missing in release namespace stalls
When `spec.serviceAccountName` is empty and `--default-service-account` is non-empty, and the named ServiceAccount does not exist in the release's namespace, the reconcile MUST stall with `Ready=False` and reason `ImpersonationFailed` (same behavior as an explicit missing SA).

#### Scenario: Default SA missing in tenant namespace
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName` empty
- **AND** the manager started with `--default-service-account=opm-deployer`
- **AND** no ServiceAccount named `opm-deployer` exists in namespace `team-a`
- **WHEN** the controller attempts to build an impersonated client
- **THEN** the reconcile is classified as `FailedStalled`
- **AND** `Ready=False` with reason `ImpersonationFailed`
- **AND** the error message indicates the default SA was not found in the release's namespace

### Requirement: Flag default applies per-release-namespace, not cross-namespace
The `--default-service-account` flag value names an SA that MUST exist in each **release's** namespace. The controller MUST NOT fall back to a ServiceAccount in the controller's namespace or any other namespace.

#### Scenario: Default SA only in controller namespace does not satisfy tenant release
- **GIVEN** the manager started with `--default-service-account=opm-deployer`
- **AND** a ServiceAccount `opm-deployer` exists in the controller's namespace `opm-system`
- **AND** no ServiceAccount named `opm-deployer` exists in namespace `team-b`
- **AND** a ModuleRelease in namespace `team-b` has `spec.serviceAccountName` empty
- **WHEN** the controller reconciles the release in `team-b`
- **THEN** the reconcile stalls with `ImpersonationFailed`
- **AND** the `opm-system/opm-deployer` SA is not used

### Requirement: Flag default applies to deletion cleanup
The same spec > flag > empty resolution used during apply and prune MUST also be used when the finalizer runs deletion cleanup. The deletion path is best-effort: if the resolved SA cannot be impersonated (missing, unauthorized, or the manager has no RestConfig), the controller MUST log the failure and fall back to its own client so the finalizer can clear. Deletion MUST NOT stall indefinitely on impersonation failure.

#### Scenario: Deletion cleanup uses flag-defaulted SA when spec is empty
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName` empty, `spec.prune=true`, and an inventory of previously-applied resources
- **AND** the manager started with `--default-service-account=opm-deployer`
- **AND** a ServiceAccount `opm-deployer` exists in namespace `team-a`
- **WHEN** the ModuleRelease is deleted and the finalizer runs cleanup
- **THEN** prune operations impersonate `system:serviceaccount:team-a:opm-deployer`
- **AND** the finalizer is removed once cleanup succeeds

#### Scenario: Deletion cleanup falls back to controller client when flag-defaulted SA is missing
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName` empty and an inventory of previously-applied resources
- **AND** the manager started with `--default-service-account=opm-deployer`
- **AND** no ServiceAccount named `opm-deployer` exists in namespace `team-a` (e.g. the SA was deleted before the release)
- **WHEN** the finalizer runs cleanup
- **THEN** the controller logs that the ServiceAccount is unavailable and falls back to the controller's own client
- **AND** the finalizer is not blocked by the missing SA
