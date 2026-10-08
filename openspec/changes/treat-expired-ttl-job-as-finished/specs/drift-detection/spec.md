## MODIFIED Requirements

### Requirement: A missing object is restored

An object that the render produces and that does not exist on the cluster is not drift, and the `Drifted` condition SHALL NOT report it. When a ModuleInstance reconcile renders, finds every digest unchanged, withholds nothing, and its dry-run shows that one or more rendered objects do not exist, the controller SHALL apply those missing objects, and only those, through the identity that applies the instance. Objects that exist SHALL NOT be applied by this step, so the `Drifted` condition that the same reconcile computed stays as computed.

A reconcile that restores an object is an apply: its outcome is `Applied`, it records a history entry, moves `status.lastAppliedAt` and judges health from that moment. A failed restore SHALL be classified and retried as a failed apply.

A `batch/v1` Job whose rendered spec sets `ttlSecondsAfterFinished` SHALL NOT be restored: the cluster deletes such a Job after it finished, and to create it again would run it again. Such a Job that does not exist while every digest is unchanged is an expired Job, and the controller SHALL treat it as finished: the reconcile SHALL remove it from the entries of `status.inventory`, so that `instance-health` does not read it, and SHALL leave `status.inventory.digest` as the digest of the rendered set, so that the next reconcile with unchanged digests is still a `NoOp`. The controller records no completion of a Job. A Job with a TTL that was removed before it ran therefore reads as expired too and is not created again until a digest changes. A reconcile whose digests changed applies every rendered object, the Job included, and records the full rendered inventory.

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
