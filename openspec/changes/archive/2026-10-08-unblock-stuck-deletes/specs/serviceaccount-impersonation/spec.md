## ADDED Requirements

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
