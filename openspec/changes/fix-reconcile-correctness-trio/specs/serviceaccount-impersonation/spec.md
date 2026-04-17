## ADDED Requirements

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
