## MODIFIED Requirements

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
- **AND** the impersonated identity is a member of the standard SA groups (`system:serviceaccounts`, `system:serviceaccounts:team-a`, `system:authenticated`)

## REMOVED Requirements

### Requirement: Impersonation includes standard SA group set
**Reason**: Its premise is wrong. For an impersonated ServiceAccount user with no group named, the apiserver adds the ServiceAccount's groups itself, so the controller need not send them, and sending them requires `impersonate` on `groups`, a right that lets the controller claim any group.
**Migration**: None for users. The impersonated identity keeps the same three groups; the requirement "Impersonated identity has the ServiceAccount's groups" states that, and "Controller may impersonate ServiceAccounts only" states the narrower role.

## ADDED Requirements

### Requirement: Impersonated identity has the ServiceAccount's groups
An impersonated request MUST reach the apiserver as the user `system:serviceaccount:<namespace>:<name>` with exactly the groups a token of that ServiceAccount carries: `system:serviceaccounts`, `system:serviceaccounts:<namespace>` and `system:authenticated`. The controller MUST NOT name a group on an impersonated request; the apiserver derives these groups from the ServiceAccount user name. An RBAC binding whose subject is one of these groups MUST authorise the impersonated request as it authorises the same ServiceAccount with its own token.

#### Scenario: Identity seen by the apiserver
- **GIVEN** a ServiceAccount `deploy-sa` in namespace `team-a`
- **WHEN** the controller's impersonated client for it asks the apiserver who it is
- **THEN** the user is `system:serviceaccount:team-a:deploy-sa`
- **AND** the groups are exactly `system:serviceaccounts`, `system:serviceaccounts:team-a` and `system:authenticated`

#### Scenario: No group is sent
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName=deploy-sa`
- **WHEN** the controller builds the impersonated client
- **THEN** the impersonation config names the user `system:serviceaccount:team-a:deploy-sa`
- **AND** it names no group

#### Scenario: Group-subject RoleBinding authorizes apply
- **GIVEN** a ModuleRelease in namespace `team-a` with `spec.serviceAccountName=deploy-sa` and a RoleBinding in `team-a` whose subjects are `[{Kind: "Group", Name: "system:serviceaccounts:team-a"}]` granting permissions on the resources to be applied
- **WHEN** the controller runs Phase 5 (Apply)
- **THEN** the apply succeeds (the impersonated identity is recognized as a member of `system:serviceaccounts:team-a`)
- **AND** `Ready=True` is set on the ModuleRelease

#### Scenario: ServiceAccount without a binding is refused
- **GIVEN** a ServiceAccount in namespace `team-a` that no RoleBinding names, directly or through a group
- **WHEN** the controller applies a resource in `team-a` as that ServiceAccount
- **THEN** the apiserver refuses the apply as forbidden
- **AND** the resource is not created

### Requirement: Controller may impersonate ServiceAccounts only
The controller's own ClusterRole MUST grant the `impersonate` verb on `serviceaccounts` and on no other resource. It MUST NOT grant `impersonate` on `users` or on `groups`. This MUST hold in every form the role ships in: the kustomize role, the install manifest and the operator module's rendered RBAC. With that role alone the apply path MUST still work.

#### Scenario: Shipped role names serviceaccounts only
- **WHEN** the rules of the shipped manager ClusterRole are read
- **THEN** the only resource with the `impersonate` verb is `serviceaccounts`

#### Scenario: Apply works under the shipped role
- **GIVEN** a controller identity that holds exactly the rules of the shipped manager ClusterRole
- **AND** a ServiceAccount in namespace `team-a` that a RoleBinding allows to manage ConfigMaps
- **WHEN** the controller applies a ConfigMap in `team-a` as that ServiceAccount
- **THEN** the apply succeeds

#### Scenario: Impersonating a user is refused
- **GIVEN** a controller identity that holds exactly the rules of the shipped manager ClusterRole
- **WHEN** it sends a request that impersonates the user `jane`, with or without a group
- **THEN** the apiserver refuses the request as forbidden

#### Scenario: Claiming a group is refused
- **GIVEN** a controller identity that holds exactly the rules of the shipped manager ClusterRole
- **WHEN** it sends a request that impersonates a ServiceAccount and names the group `system:masters`
- **THEN** the apiserver refuses the request as forbidden
