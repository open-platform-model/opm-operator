## ADDED Requirements

### Requirement: A missing object is restored

An object that the render produces and that does not exist on the cluster is not drift, and the `Drifted` condition SHALL NOT report it. When a ModuleInstance reconcile renders, finds every digest unchanged, withholds nothing, and its dry-run shows that one or more rendered objects do not exist, the controller SHALL apply those missing objects, and only those, through the identity that applies the instance. Objects that exist SHALL NOT be applied by this step, so the `Drifted` condition that the same reconcile computed stays as computed.

A reconcile that restores an object is an apply: its outcome is `Applied`, it records a history entry, moves `status.lastAppliedAt` and judges health from that moment. A failed restore SHALL be classified and retried as a failed apply.

A `batch/v1` Job whose rendered spec sets `ttlSecondsAfterFinished` SHALL NOT be restored: the cluster deletes such a Job after it finished, and to create it again would run it again.

A reconcile that skips its render has no rendered objects and SHALL NOT restore anything; the drift render interval (`--drift-render-interval`) bounds how long a missing object waits. A failed dry-run SHALL leave the missing set unknown, and nothing is restored on that reconcile.

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

#### Scenario: Nothing is missing

- **GIVEN** a Ready ModuleInstance whose rendered objects all exist
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** no apply is sent and the outcome is `NoOp`
