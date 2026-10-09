## ADDED Requirements

### Requirement: An object adopted by another instance is excluded from drift detection and from the restore
Drift detection MUST compare only the objects the apply verdict allows, without the objects taken in by the same reconcile ("A missing object is restored"). An object the verdict refuses as `adopted-elsewhere` MUST NOT be reported as drifted or as missing, and MUST NOT be restored. The read the verdict needs MUST be the one read made per object before the dry-run, so a reconcile that renders sends no more requests per object than before. A reconcile with unchanged digests whose read of an object fails MUST report that as a failed drift check, as before (`Drifted=Unknown` with reason `DriftCheckForbidden` when the read is Forbidden), MUST restore nothing and MUST let no object go in that reconcile. Source: 0012:D8:R8.

#### Scenario: A let-go object is not drift
- **GIVEN** an inventoried ConfigMap whose `opmodel.dev/adopt` annotation names another instance and whose data the other instance changed
- **WHEN** a reconcile renders with unchanged digests
- **THEN** `Drifted` does not name the ConfigMap

#### Scenario: A let-go object is not restored
- **GIVEN** a ConfigMap the instance let go, which the other instance then deleted and created again with its annotation
- **WHEN** the controller reconciles
- **THEN** the controller does not write the ConfigMap

#### Scenario: One read per object
- **GIVEN** a ModuleInstance with ten rendered objects and unchanged digests
- **WHEN** a reconcile renders
- **THEN** the controller sends one GET of its own per object before the dry-run, as before this requirement

#### Scenario: A refused read on unchanged digests
- **GIVEN** an effective ServiceAccount that may not get ConfigMaps, and unchanged digests
- **WHEN** the controller reconciles
- **THEN** `Drifted` is `Unknown` with reason `DriftCheckForbidden`, nothing is restored and `status.inventory` keeps its entries

## MODIFIED Requirements

### Requirement: A missing object is restored

An object that the render produces and that does not exist on the cluster is not drift, and the `Drifted` condition SHALL NOT report it. When a ModuleInstance reconcile renders, finds every digest unchanged, withholds nothing, and its dry-run shows that one or more rendered objects do not exist, the controller SHALL apply those missing objects through the identity that applies the instance. The same step SHALL apply the rendered objects that are taken in: objects that exist, are not in `status.inventory` and that the apply verdict allows (`reconcile-loop-assembly`, "An allowed object outside the inventory is taken in"), also when nothing is missing. It SHALL apply these two sets and only these. Objects that exist and are in `status.inventory` SHALL NOT be applied by this step, so the `Drifted` condition that the same reconcile computed stays as computed. A taken-in object SHALL be left out of the dry-run diff of the reconcile that takes it in, so `Drifted` does not name an object that the same reconcile applies; from the next render on it is compared like every other inventoried object.

A reconcile that restores an object is an apply: its outcome is `Applied`, it records a history entry, moves `status.lastAppliedAt` and judges health from that moment. A failed restore SHALL be classified and retried as a failed apply.

A `batch/v1` Job whose rendered spec sets `ttlSecondsAfterFinished` SHALL NOT be restored: the cluster deletes such a Job after it finished, and to create it again would run it again. Such a Job that does not exist while every digest is unchanged is an expired Job, and the controller SHALL treat it as finished: the reconcile SHALL remove it from the entries of `status.inventory`, so that `instance-health` does not read it, and SHALL leave `status.inventory.digest` as the digest of the rendered set, so that the next reconcile with unchanged digests is still a `NoOp`. The controller records no outcome of a Job. A Job with a TTL that was removed before it ran, and one that failed before the cluster removed it, therefore read as expired too: neither is created again until a digest changes, and the failure is no longer reported on the instance once the Job is gone. A reconcile whose digests changed applies every rendered object, the Job included, and records the full rendered inventory.

A reconcile that skips its render has no rendered objects and SHALL NOT restore anything; the drift render interval (`--drift-render-interval`) bounds how long a missing object waits. A missing Job waits less: a ModuleInstance reconcile that reads an inventory Job as absent does not skip its render (`render-input-key`), so the Job is restored, or removed from the inventory as expired, on that reconcile. A failed dry-run SHALL leave the missing set unknown, and nothing is restored on that reconcile.

#### Scenario: A deleted object is created again

- **GIVEN** a Ready ModuleInstance whose ConfigMap `foo` was deleted by hand
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** ConfigMap `foo` exists again with the rendered content
- **AND** the outcome is `Applied` and `status.lastAppliedAt` moves

#### Scenario: Only the missing object is applied

- **GIVEN** a Ready ModuleInstance with ConfigMap `foo` deleted and ConfigMap `bar` modified by hand
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** `foo` is created again
- **AND** `bar` keeps its modified content and `Drifted=True` reports it

#### Scenario: A finished Job with a TTL stays absent

- **GIVEN** a Ready ModuleInstance whose rendered Job sets `ttlSecondsAfterFinished` and no longer exists
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** the Job is not created and the outcome is `NoOp`
- **AND** `status.inventory.entries` no longer lists the Job, and `status.inventory.digest` and `status.inventory.revision` keep their values
- **AND** `Healthy` is `True` with reason `RolledOut` when the other inventory objects are healthy

#### Scenario: A Job with a TTL that was removed before it ran

- **GIVEN** a Ready ModuleInstance whose rendered Job sets `ttlSecondsAfterFinished` and was deleted before any Pod of it ran
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** the Job is not created, because the controller cannot tell it from a Job that finished
- **AND** the Job leaves `status.inventory.entries` and does not make the instance unhealthy

#### Scenario: A Job with a TTL that failed and expired

- **GIVEN** a ModuleInstance with `Healthy=False` because its rendered Job with `ttlSecondsAfterFinished` has condition `Failed`
- **WHEN** the cluster removes the Job and the controller reconciles with unchanged digests
- **THEN** the Job is not created, it leaves `status.inventory.entries`, and `Healthy` is judged over the remaining entries

#### Scenario: A missing Job without a TTL is created again

- **GIVEN** a Ready ModuleInstance whose rendered Job sets no `ttlSecondsAfterFinished` and was deleted
- **WHEN** the controller reconciles
- **THEN** the reconcile renders, the Job is created again and the outcome is `Applied`
- **AND** `status.inventory.entries` still lists the Job

#### Scenario: An expired Job beside a deleted object

- **GIVEN** a Ready ModuleInstance whose Job with a TTL expired and whose ConfigMap `foo` was deleted by hand
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** `foo` is created again, the Job is not, and the outcome is `Applied`
- **AND** the new `status.inventory` lists `foo` and not the Job, with the digest of the rendered set

#### Scenario: A restore that fails

- **GIVEN** a Ready ModuleInstance whose ConfigMap `foo` was deleted by hand
- **WHEN** the controller reconciles, renders with unchanged digests, and the apply of `foo` fails
- **THEN** `Ready` is `False` with reason `ApplyFailed`, the reconcile requeues on the transient backoff and `status.nextRetryAt` is set

#### Scenario: Nothing is missing

- **GIVEN** a Ready ModuleInstance whose rendered objects all exist
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** no apply is sent and the outcome is `NoOp`

#### Scenario: A taken-in object is applied by the restore step

- **GIVEN** a Ready ModuleInstance with unchanged digests, a rendered ConfigMap `foo` that exists outside `status.inventory` with the instance's adopt annotation and other data than the render, and an inventoried ConfigMap `bar` modified by hand
- **WHEN** the controller reconciles and renders
- **THEN** `foo` has the rendered content and is listed in `status.inventory`, and the outcome is `Applied`
- **AND** `bar` keeps its modified content, and `Drifted=True` names `bar` and does not name `foo`
