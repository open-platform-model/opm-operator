## Purpose

Define how the operator applies, prunes and deletes as the ServiceAccount a release names: which ServiceAccount is effective, what the controller's own role may do with ServiceAccounts, and how a missing or unauthorised ServiceAccount stalls and recovers.

## Requirements

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

### Requirement: Deletion-cleanup SA-missing stalls with distinct reason
When the deletion cleanup path attempts to build an impersonated client and the target ServiceAccount does not exist (apiserver returns NotFound for the SA lookup), the controller MUST:

- NOT silently fall back to the controller's own client.
- Stall the release with `Ready=False` and condition reason `DeletionSAMissing` (distinct from the generic `ImpersonationFailed` used on the apply path).
- Populate the condition message with the SA's namespaced name and list the operator recovery options (restore SA, set `spec.prune=false`, or set the orphan annotation).
- Retain the finalizer.
- Emit a `Warning` event with reason `DeletionSAMissing` on the Ready transition (not on every requeue).
- Requeue on the stalled-recheck interval, not the tight transient backoff.

This behavior MUST apply symmetrically to both `ModuleRelease` and `Release` deletion paths.

#### Scenario: SA deleted before finalizer can prune
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=hello-applier`, `spec.prune=true`, and a non-empty inventory
- **AND** the ServiceAccount `hello-applier` has been deleted from the release's namespace
- **WHEN** the ModuleRelease is deleted and the finalizer runs deletion cleanup
- **THEN** the impersonated client build fails with SA-NotFound
- **AND** the release's Ready condition becomes False with reason `DeletionSAMissing`
- **AND** the condition message names `default/hello-applier` and lists recovery options
- **AND** a Warning event is emitted with reason `DeletionSAMissing`
- **AND** the finalizer is NOT removed
- **AND** the controller client is NOT used as a fallback for prune

#### Scenario: Other impersonation errors keep existing behavior
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=deploy-sa` and the controller lacks `impersonate` RBAC
- **WHEN** the ModuleRelease is deleted and the finalizer runs deletion cleanup
- **THEN** the release stalls with reason `ImpersonationFailed` (the existing generic reason)
- **AND** the reason is NOT `DeletionSAMissing`
- **AND** the orphan annotation has no effect on this case

### Requirement: Orphan-exit annotation removes finalizer on SA-missing
When a release is in the `DeletionSAMissing` stall state AND the annotation `opm.dev/force-delete-orphan` is set to `"true"` on the release, the controller MUST:

- Skip prune entirely.
- Remove the finalizer.
- Emit a `Warning` event with reason `OrphanedOnDeletion` naming the number of inventory entries left behind.
- Clear `status.inventory` in the final status patch so the last-observed state does not claim ownership of resources the controller is abandoning.

The annotation MUST only take effect when the deletion cleanup impersonation failure is SA-NotFound. For any other impersonation error (transient, RBAC-denied, etc.), the annotation MUST be ignored and the existing stall behavior preserved.

Any annotation value other than the literal string `"true"` MUST be treated as absent.

#### Scenario: Orphan annotation releases a stuck deletion
- **GIVEN** a ModuleRelease stalled with reason `DeletionSAMissing` and `status.inventory` listing 3 entries
- **WHEN** an operator patches the release with annotation `opm.dev/force-delete-orphan=true`
- **AND** the next reconcile runs
- **THEN** prune is not attempted
- **AND** a Warning event is emitted with reason `OrphanedOnDeletion` and message referencing 3 orphaned entries
- **AND** the finalizer is removed
- **AND** the release object is garbage-collected by the apiserver
- **AND** the 3 previously-managed resources remain in the cluster

#### Scenario: Orphan annotation ignored for non-SA-missing stall
- **GIVEN** a ModuleRelease stalled with reason `ImpersonationFailed` (RBAC-denied impersonate verb, not SA-missing)
- **AND** the annotation `opm.dev/force-delete-orphan=true` is set
- **WHEN** the next reconcile runs
- **THEN** the annotation has no effect
- **AND** the release remains stalled with `ImpersonationFailed`
- **AND** the finalizer is NOT removed

#### Scenario: Annotation value other than "true" is treated as absent
- **GIVEN** a ModuleRelease stalled with reason `DeletionSAMissing`
- **AND** the annotation `opm.dev/force-delete-orphan=yes` is set (value is not the literal `"true"`)
- **WHEN** the next reconcile runs
- **THEN** the annotation is ignored
- **AND** the release remains stalled with `DeletionSAMissing`
- **AND** the finalizer is NOT removed

### Requirement: A created ServiceAccount wakes the instances that impersonate it

When a ServiceAccount is created, the controller MUST enqueue every `ModuleInstance` in that ServiceAccount's namespace whose effective ServiceAccount (`spec.serviceAccountName`, else the manager's `--default-service-account`) has that name and that is stalled on it: its `Ready` condition is `False` with reason `DeletionSAMissing` or `ImpersonationFailed`. Such an instance then recovers without waiting for its stalled recheck. This holds for an instance being deleted and for one stalled on the apply path.

The controller MUST NOT enqueue an instance that is not stalled on its ServiceAccount, whatever ServiceAccount is created. A user who may create ServiceAccounts in a namespace, but no `ModuleInstance`, MUST NOT be able to make a healthy instance reconcile.

The controller MUST NOT enqueue an instance the ServiceAccount cannot change: a CLI-owned instance, an instance that names another ServiceAccount, an instance in another namespace, and a suspended instance that is not being deleted. An update or a deletion of a ServiceAccount MUST NOT enqueue anything.

The restore of a ServiceAccount's RBAC alone (a Role or a binding) is not a trigger; an instance stalled with `ImpersonationFailed` on a forbidden request still waits for its stalled recheck.

#### Scenario: A created ServiceAccount enqueues the instances that name it

- **GIVEN** instance `a` with `spec.serviceAccountName: deploy-sa` and instance `b` with `spec.serviceAccountName: other-sa`, both in namespace `team-a` and both stalled with reason `ImpersonationFailed`
- **WHEN** the ServiceAccount `deploy-sa` is created in `team-a`
- **THEN** instance `a` is enqueued and instance `b` is not

#### Scenario: The flag default counts as the effective ServiceAccount

- **GIVEN** a manager started with `--default-service-account=opm-deployer` and an instance in `team-a` with no `spec.serviceAccountName`, stalled with reason `ImpersonationFailed`
- **WHEN** the ServiceAccount `opm-deployer` is created in `team-a`
- **THEN** that instance is enqueued

#### Scenario: Instances the ServiceAccount cannot change are not enqueued

- **GIVEN** a CLI-owned instance, a suspended instance that is not being deleted, and an instance in namespace `team-b`, all with `spec.serviceAccountName: deploy-sa` and all stalled with reason `ImpersonationFailed`
- **WHEN** the ServiceAccount `deploy-sa` is created in `team-a`
- **THEN** none of them is enqueued

#### Scenario: A healthy instance is not enqueued

- **GIVEN** an instance in `team-a` with `spec.serviceAccountName: deploy-sa` whose `Ready` condition is `True`, and one that was never reconciled
- **WHEN** the ServiceAccount `deploy-sa` is created in `team-a`, once or many times
- **THEN** neither instance is enqueued

### Requirement: Controller may list and watch ServiceAccounts

The controller's own ClusterRole MUST grant `list` and `watch` on `serviceaccounts`, beside `get` and `impersonate`, in every form the role ships in: the kustomize role, the install manifest and the operator module's rendered RBAC. The controller MUST watch ServiceAccount metadata only. The role MUST NOT grant a write verb on `serviceaccounts`.

#### Scenario: Shipped role lets the controller watch ServiceAccounts

- **WHEN** the rules of the shipped manager ClusterRole are read
- **THEN** the verbs on `serviceaccounts` are exactly `get`, `impersonate`, `list` and `watch`
