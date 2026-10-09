## MODIFIED Requirements

### Requirement: No-op detection
The reconciler MUST detect no-op reconciliations and skip apply/prune when nothing changed. A reconcile is a no-op only when every digest matches, no restorable rendered object is missing from the cluster (`drift-detection`, "A missing object is restored"), and no rendered object that the apply verdict allows exists outside `status.inventory` (`reconcile-loop-assembly`, "An allowed object outside the inventory is taken in").

#### Scenario: All digests match
- **WHEN** source, config, render, and inventory digests all match the last applied values
- **AND** no restorable rendered object is missing from the cluster
- **AND** every rendered object that exists is in `status.inventory` or is adopted by another instance
- **THEN** the controller skips apply and prune, keeps `Ready=True`, and does not record a new history entry

#### Scenario: All digests match and an object is missing
- **WHEN** all four digests match the last applied values and a restorable rendered object does not exist on the cluster
- **THEN** the controller applies the missing object, prunes nothing, and records a history entry

### Requirement: Inventory updated only on full success
The `status.inventory` MUST only be replaced after a fully successful apply (and prune, if enabled). Two narrower writes are allowed. A ModuleInstance reconcile whose digests are unchanged MUST remove the entries of expired Jobs (`drift-detection`, "A missing object is restored") from `status.inventory.entries`, on a `NoOp` and on a restore. A reconcile that renders MUST leave out of the inventory it records, on a `NoOp` as well, the entry of every object the apply verdict refuses as `adopted-elsewhere` (0012:D8:R8). `status.inventory.digest` MUST stay the digest of the rendered set, so after such a removal it is not the digest of the entries listed.

A reconcile that is refused by the apply verdict, or that fails, MUST NOT change `status.inventory`.

#### Scenario: Partial failure preserves inventory
- **WHEN** apply succeeds but prune fails
- **THEN** `status.inventory` remains at the previous successful value

#### Scenario: An expired Job leaves the inventory without an apply
- **WHEN** a reconcile renders with unchanged digests, sends no apply, and a rendered Job that sets `ttlSecondsAfterFinished` does not exist
- **THEN** `status.inventory.entries` no longer lists the Job
- **AND** the next reconcile with unchanged digests is a `NoOp`

#### Scenario: An adopted object leaves the inventory without an apply
- **GIVEN** a Ready ModuleInstance and an inventoried ConfigMap that a user annotates `opmodel.dev/adopt` with another instance's UUID
- **WHEN** a reconcile renders with unchanged digests
- **THEN** it sends no apply, `status.inventory.entries` no longer lists the ConfigMap and `Ready` stays `True`
- **AND** the next reconcile with unchanged digests is a `NoOp`

#### Scenario: A refused reconcile keeps the inventory
- **WHEN** the apply verdict refuses a reconcile
- **THEN** `status.inventory` holds the entries it held before

## ADDED Requirements

### Requirement: A reconcile the apply verdict refuses is retried and not stalled
When the apply verdict refuses a reconcile (`ssa-apply`, "An apply is judged by the ownership verdict before its first write"), the reconciler MUST set `Ready=False` with reason `ApplyRefused`, MUST NOT set `Stalled`, MUST count the attempt as a failed apply and MUST retry on the bounded backoff. `status.inventory`, the applied digests, `status.instanceUUID` and `status.previousInstanceUUID` MUST keep their values. A reconcile whose digests are unchanged MUST NOT refuse because an inventoried object is being deleted: it writes nothing over that object.

#### Scenario: A refusal is transient
- **GIVEN** a changed render that names a live object OPM does not manage
- **WHEN** the controller reconciles
- **THEN** `Ready` is `False` with reason `ApplyRefused`, `Stalled` is absent, and the reconcile is requeued on the backoff

#### Scenario: The refusal ends when the object is adopted
- **GIVEN** the instance of the scenario above
- **WHEN** a user annotates the object `opmodel.dev/adopt` with the instance's UUID and the retry runs
- **THEN** the render is applied and `Ready` is `True`

#### Scenario: A refused identity change stores nothing
- **GIVEN** a ModuleInstance whose `spec.module.path` changed, and a rendered object that the verdict refuses
- **WHEN** the controller reconciles
- **THEN** `status.instanceUUID` keeps the earlier identity and `status.previousInstanceUUID` stays empty

#### Scenario: An inventoried object being deleted on unchanged digests
- **GIVEN** a Ready ModuleInstance with unchanged digests and an inventoried Deployment that carries a deletion timestamp
- **WHEN** the controller reconciles
- **THEN** `Ready` does not change to `ApplyRefused`

#### Scenario: A rendered object held by another instance on unchanged digests
- **GIVEN** a ModuleInstance with unchanged digests whose render names an object that exists outside its inventory and carries another instance's UUID label and no adopt annotation
- **WHEN** the controller reconciles
- **THEN** nothing is written and `Ready` is `False` with reason `ApplyRefused`

### Requirement: An allowed object outside the inventory is taken in
When a rendered object exists, is not in `status.inventory`, and the apply verdict allows it, the reconciler MUST apply it and MUST record it in `status.inventory` in the same reconcile, also when every digest is unchanged. With unchanged digests a ModuleInstance MUST apply only such objects and the restorable missing objects, as a restore does, and MUST NOT rewrite an object that is in the inventory. Source: 0012:D8:R2.

#### Scenario: An adopted object is taken in on unchanged digests
- **GIVEN** a Ready ModuleInstance whose render names a ConfigMap it let go earlier, and a user who sets the ConfigMap's `opmodel.dev/adopt` annotation to this instance's UUID
- **WHEN** a reconcile renders with unchanged digests
- **THEN** the ConfigMap is applied and listed in `status.inventory`
- **AND** no other existing object is written

#### Scenario: A kept claim is taken back
- **GIVEN** a PersistentVolumeClaim that a prune kept, so it is out of the inventory and carries this instance's labels, and a render that names it again
- **WHEN** the controller reconciles
- **THEN** the claim is applied and listed in `status.inventory`
