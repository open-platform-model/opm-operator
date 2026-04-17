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
When `spec.serviceAccountName` is empty, the controller MUST use its own client.

#### Scenario: No impersonation when SA not specified
- **GIVEN** a ModuleRelease with `spec.serviceAccountName` empty or unset
- **WHEN** the controller reconciles
- **THEN** all operations use the controller's own service account

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
