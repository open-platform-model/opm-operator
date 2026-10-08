## REMOVED Requirements

### Requirement: Namespace safety exclusion
**Reason**: The rule is the library's and matches on group and kind; the requirement "Kinds OPM never deletes are the library's" replaces it.
**Migration**: None for a core `Namespace`. A custom resource of a kind named `Namespace` in another API group is pruned like any other resource.

### Requirement: CRD safety exclusion
**Reason**: The rule is the library's and matches on group and kind; the requirement "Kinds OPM never deletes are the library's" replaces it.
**Migration**: None for a `CustomResourceDefinition` of `apiextensions.k8s.io`. A resource of that kind name in another API group is pruned like any other resource.

### Requirement: Live-state UUID-based ownership guard
**Reason**: The operator no longer compares labels itself. The requirement "Prune asks the library's delete verdict" replaces it and adds the adopt annotation and the delete precondition.
**Migration**: An object whose `opmodel.dev/adopt` annotation names another instance is left in the cluster. Every other outcome of the earlier guard is kept: a missing or foreign managed-by label and a UUID label of another instance skip the delete, and an object without a UUID label is deleted on its managed-by label.

### Requirement: Release UUID persisted on ModuleReleaseStatus
**Reason**: It names a kind and a field that no longer exist. The requirement "The prune judges with the recorded identities" here and the requirement "The instance identities are stored before the first write of an apply" of `reconcile-loop-assembly` replace it.
**Migration**: None. `status.instanceUUID` keeps its name on ModuleInstance.

## ADDED Requirements

### Requirement: Prune asks the library's delete verdict
For every entry it may delete, the prune MUST read the live object with the client that would delete it and MUST ask the library's delete verdict (`opm/k8s/ownership`) with that object and an identity of the instance. When the instance has more than one identity to judge with, it MUST ask with each in turn, and the object counts as the instance's own when the verdict says proceed for one of them. It MUST delete the object only then, and it MUST NOT decide ownership with a label or annotation comparison of its own. Source: 0012:D4:R1, 0012:D8:R8.

The prune MUST act on each answer as follows:

- Proceed: the object is deleted, unless it is a PersistentVolumeClaim that `spec.dataPolicy` keeps. A claim counts as kept only after the verdict said proceed.
- The object does not exist: success, as before.
- For every identity, the object is not managed by OPM, belongs to another instance, or carries an adopt annotation that names another instance: the object is left in the cluster, counted as skipped and named in the prune result with the library's reason and message. It is not an error.
- The read fails with an error other than NotFound: the entry is a failed prune and the remaining entries are still attempted, as before. A PersistentVolumeClaim that cannot be read while `spec.dataPolicy` keeps claims is kept without an error, as before.

An object without a UUID label MUST still be deleted when OPM manages it, and every OPM manager label value MUST be accepted, as before.

#### Scenario: Object not managed by OPM is left
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object has no `app.kubernetes.io/managed-by` label
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap still exists
- **AND** the prune result names it with the reason `not-opm-managed`

#### Scenario: Object of another instance is left
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object is managed by OPM and carries the UUID label of another instance
- **WHEN** the controller prunes the stale set with this instance's identity
- **THEN** the ConfigMap still exists
- **AND** the prune result names it with the reason `owner-mismatch`

#### Scenario: Object being adopted by another instance is left
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object carries this instance's UUID label and the annotation `opmodel.dev/adopt` with the UUID of another instance
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap still exists
- **AND** the prune result names it with the reason `adopted-elsewhere`

#### Scenario: Own object is deleted
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object is managed by OPM and carries this instance's UUID label and no adopt annotation
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap is deleted and counted as deleted

#### Scenario: Object without a UUID label is deleted
- **GIVEN** a stale entry for ConfigMap `team-a/legacy` whose live object carries `app.kubernetes.io/managed-by=open-platform-model` and no UUID label
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap is deleted

#### Scenario: Object still carrying the cli manager identity is deleted
- **GIVEN** a stale entry for ConfigMap `team-a/example` whose live object carries `app.kubernetes.io/managed-by=opm-cli` and this instance's UUID label
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap is deleted

#### Scenario: A failed read fails the entry and not the run
- **GIVEN** a stale set of two ConfigMaps and an API server that refuses the read of the first
- **WHEN** the controller prunes the stale set
- **THEN** the second ConfigMap is deleted
- **AND** the prune returns an error that names the first

### Requirement: Kinds OPM never deletes are the library's
The prune MUST NOT delete an object of a kind the library's ownership package excludes from deletion: a `Namespace` of the core group and a `CustomResourceDefinition` of `apiextensions.k8s.io`. The match MUST be on group and kind. Such an entry MUST be left without a read, counted as skipped and named in the prune result with the reason `safety-excluded`.

#### Scenario: Namespace in the stale set
- **WHEN** a core `Namespace` is in the stale set
- **THEN** it is not deleted and the prune result names it with the reason `safety-excluded`

#### Scenario: CustomResourceDefinition in the stale set
- **WHEN** a `CustomResourceDefinition` of `apiextensions.k8s.io` is in the stale set
- **THEN** it is not deleted and the prune result names it with the reason `safety-excluded`

#### Scenario: The same kind name in another group
- **GIVEN** a stale entry of kind `Namespace` in the group `example.com`, live and owned by the instance
- **WHEN** the controller prunes the stale set
- **THEN** the object is deleted

### Requirement: Deletes carry the UID precondition
Every DELETE the prune sends MUST carry a precondition on the UID of the live object the verdict judged. A DELETE that the API server refuses on that precondition MUST be a failed prune for that entry: it MUST NOT be counted as deleted, the object that now holds the name MUST NOT be deleted in that run, and the remaining entries MUST still be attempted.

#### Scenario: The judged object is deleted
- **GIVEN** a stale ConfigMap owned by the instance
- **WHEN** the controller prunes it
- **THEN** the DELETE request names the UID of the object that was read

#### Scenario: An object replaced since the read survives
- **GIVEN** a stale ConfigMap that is deleted and created again under the same name after the prune read it and before the prune deletes it
- **WHEN** the prune sends its DELETE
- **THEN** the new ConfigMap still exists
- **AND** the prune returns an error for that entry and counts nothing as deleted for it

### Requirement: The prune judges with the recorded identities
The prune of stale resources MUST judge with the identities the status held when the apply of the same reconcile started: `status.instanceUUID`, and `status.previousInstanceUUID` when it is set. A stale object that carries either identity, and that the verdict lets the operator delete under it, MUST be deleted. Source: owner decisions of 2026-10-08 (prune judges with the identity stored in the instance's record, also after the instance identity changed; the status keeps both identities until the prune succeeded).

When `status.instanceUUID` was empty when the reconcile started, the prune MUST ask the verdict with the render's identity first and then with no identity, so that it deletes what the managed-by label alone let it delete before.

#### Scenario: Stale object after the instance identity changed
- **GIVEN** a ModuleInstance with `status.instanceUUID` `A` and `spec.prune=true`, whose `spec.module.path` changes so that its render carries identity `B` and no longer holds ConfigMap `team-a/old`, which carries the UUID label `A`
- **WHEN** the reconcile applies and prunes
- **THEN** ConfigMap `team-a/old` is deleted
- **AND** after the reconcile `status.instanceUUID` is `B` and `status.previousInstanceUUID` is empty

#### Scenario: A failed prune leaves nothing orphaned on the retry
- **GIVEN** the same change of identity, a second stale ConfigMap `team-a/older` with the UUID label `A`, and a prune whose DELETE of `team-a/old` fails
- **WHEN** the reconcile ends and the next reconcile runs
- **THEN** after the first reconcile `status.instanceUUID` is `B`, `status.previousInstanceUUID` is `A` and `status.inventory` is unchanged
- **AND** the second reconcile deletes `team-a/old`, and `team-a/older` if it still exists
- **AND** after it `status.previousInstanceUUID` is empty

#### Scenario: A stale object that was already relabelled
- **GIVEN** an identity change from `A` to `B` that is not settled, and a later render of identity `B` that drops Deployment `team-a/app`, which the earlier apply relabelled to `B`
- **WHEN** the reconcile applies and prunes
- **THEN** Deployment `team-a/app` is deleted

#### Scenario: Stale object that carries a third identity
- **GIVEN** a stale ConfigMap whose live UUID label is neither of the recorded identities nor empty
- **WHEN** the controller prunes the stale set
- **THEN** the ConfigMap still exists and the prune result names it with the reason `owner-mismatch`

#### Scenario: No recorded identity, object of an unknown earlier identity
- **GIVEN** an object with an inventory and an empty `status.instanceUUID`, whose render carries identity `B`, and a stale ConfigMap that is managed by OPM, carries the UUID label `X` and has no adopt annotation
- **WHEN** the reconcile applies and prunes
- **THEN** the ConfigMap is deleted

#### Scenario: No recorded identity, object annotated for this instance
- **GIVEN** the same object, and a stale ConfigMap that is managed by OPM, carries no UUID label and carries the annotation `opmodel.dev/adopt` with the value `B`
- **WHEN** the reconcile applies and prunes
- **THEN** the ConfigMap is deleted

#### Scenario: No recorded identity, object annotated for another instance
- **GIVEN** the same object, and a stale ConfigMap that carries the annotation `opmodel.dev/adopt` with the value `C`
- **WHEN** the reconcile applies and prunes
- **THEN** the ConfigMap still exists and the prune result names it with the reason `adopted-elsewhere`

### Requirement: The prune result names what was left behind
The prune result MUST name every entry the prune left in the cluster because the verdict skipped it, a safety-excluded kind included, with the library's reason and message for it. An entry that was already absent and a kept PersistentVolumeClaim MUST NOT be named there. An entry left behind MUST leave `status.inventory` as a deleted entry does, and MUST NOT make the reconcile fail.

#### Scenario: Mixed prune
- **GIVEN** a stale set with an own ConfigMap, a ConfigMap of another instance, a core Namespace and an entry that no longer exists
- **WHEN** the controller prunes the stale set
- **THEN** the result counts one deleted and two skipped
- **AND** it names the ConfigMap of another instance and the Namespace, each with its reason and message

#### Scenario: Inventory after an object was left behind
- **GIVEN** a ModuleInstance whose prune left ConfigMap `team-a/example` behind
- **WHEN** the reconcile completes
- **THEN** `status.inventory.entries` does not list `team-a/example`
- **AND** the `Ready` condition is True
