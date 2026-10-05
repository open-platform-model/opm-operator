## MODIFIED Requirements

### Requirement: A reconcile skips its render when its inputs are unchanged

Before rendering, a ModuleInstance or ModulePackage reconcile SHALL compute the key from the object (for a ModulePackage, after resolving its source), `Platform.status.packageIdentity`, the resolved `Platform.spec.skewPolicy` of the `cluster` Platform, and the running operator and library versions. It SHALL skip the render when, and only when, all of these hold:

- the drift render interval is greater than zero;
- `status.lastAppliedInputs` is set and its `renderedAt` is less than the drift render interval ago and not in the future;
- `Ready` is `True` with reason `ReconciliationSucceeded`;
- `status.observedGeneration` equals `metadata.generation`;
- the key is complete and its digest equals `status.lastAppliedInputs.digest`;
- for a ModulePackage, the source just resolved (ref, artifact revision, digest and URL) equals `status.source`.

A skipped reconcile SHALL NOT take a render slot, lease the platform, fetch the source artifact, render, run drift detection, apply, prune or emit an event. It is not a reconcile attempt: it records no outcome, no history and no `lastAttempted*`, and it SHALL NOT move `renderedAt`. It SHALL judge health over `status.inventory` (`instance-health`), and its only status patch SHALL be a changed `Healthy` condition. A skipped ModuleInstance reconcile SHALL return without a requeue unless health asks for one; a skipped ModulePackage reconcile SHALL requeue after `spec.interval`, as a `NoOp` does, or sooner when health asks for it.

Rationale: a render is the most expensive step of a reconcile, and when every input that shapes it is unchanged it can only reproduce what the last apply wrote; drift is still caught by the render that the drift render interval forces. The `Healthy` condition is the only status a skip writes; it reads the inventory objects and, under impersonation, the ServiceAccount, because a requeue that waits for a rollout must be able to observe it without rendering.

#### Scenario: Unchanged inputs skip the render

- **WHEN** a Ready, rolled-out (`Healthy=True`) ModuleInstance whose recorded key matches its current inputs, rendered 5 minutes ago, is reconciled with a 30-minute interval
- **THEN** the renderer is not called
- **AND** the instance's status is not patched

#### Scenario: A changed input renders

- **WHEN** the Platform's `status.packageIdentity` moves from `gen-3` to `gen-4` and the instance is reconciled within the interval
- **THEN** the instance renders

#### Scenario: A failed attempt renders on the next reconcile

- **WHEN** an instance's last attempt failed (`Ready=False`) and its spec was reverted to the inputs of the recorded key
- **THEN** the next reconcile renders

#### Scenario: A spec edit outside the key renders

- **WHEN** only a field outside the key changes (`spec.prune`, `spec.serviceAccountName`, `spec.rollout`), so `metadata.generation` moves and the key does not
- **THEN** the reconcile renders and the commit writes `status.observedGeneration`

#### Scenario: A new revision with the same digest renders once

- **WHEN** a ModulePackage's Flux source moves to a new revision whose artifact digest is unchanged, within the drift render interval
- **THEN** the package renders, the outcome is `NoOp`, and `status.source.artifactRevision` names the new revision

#### Scenario: A skipped package keeps its interval

- **WHEN** a ModulePackage whose artifact digest and other inputs are unchanged is requeued by its `spec.interval` within the drift render interval
- **THEN** the artifact is not fetched and the package is not rendered
- **AND** the reconcile requeues after `spec.interval`

#### Scenario: A skipped instance that has not rolled out requeues

- **WHEN** a Ready ModuleInstance whose recorded key matches its current inputs is `NotRolledOut` and its render is skipped
- **THEN** the renderer is not called, health is judged, and the reconcile requeues after the health requeue
