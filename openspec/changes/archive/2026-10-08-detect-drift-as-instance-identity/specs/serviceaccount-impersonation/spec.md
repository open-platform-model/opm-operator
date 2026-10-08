## MODIFIED Requirements

### Requirement: Impersonated client for apply and prune
When `spec.serviceAccountName` is set, the controller MUST use an impersonated client for apply (Phase 5) and prune (Phase 6) operations, and for the dry-run of drift detection (Phase 4) of a ModuleInstance. One reconcile MUST use one client for the three.

#### Scenario: Apply with impersonation
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=deploy-sa` in namespace `team-a`
- **WHEN** the controller runs Phase 5 (Apply)
- **THEN** SSA apply operations use the identity `system:serviceaccount:team-a:deploy-sa`
- **AND** the apply succeeds only if `deploy-sa` has sufficient RBAC permissions

#### Scenario: Prune with impersonation
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=deploy-sa` and stale resources to prune
- **WHEN** the controller runs Phase 6 (Prune)
- **THEN** delete operations use the impersonated identity

#### Scenario: Drift detection with impersonation
- **GIVEN** a ModuleInstance with `spec.serviceAccountName=deploy-sa` in namespace `team-a`
- **WHEN** the controller runs drift detection in Phase 4
- **THEN** the dry-run uses the identity `system:serviceaccount:team-a:deploy-sa`
- **AND** the controller's own identity sends no dry-run for that instance
