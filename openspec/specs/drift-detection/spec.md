# drift-detection Specification

## Purpose

Drift detection tells an operator whether the live cluster still matches what a ModuleInstance or ModulePackage last rendered. The controller compares the two with a server-side apply dry-run, reports the result on the `Drifted` condition, and never corrects drift on its own.

## Requirements

### Requirement: Drift detection via SSA dry-run
The controller MUST perform SSA dry-run in Phase 4 to detect whether live cluster state differs from desired state.

#### Scenario: No drift detected
- **GIVEN** a ModuleRelease whose rendered resources match the live cluster state
- **WHEN** the controller runs Phase 4 (Plan Actions)
- **THEN** the `Drifted` condition is not set (or set to `False`)

#### Scenario: Drift detected
- **GIVEN** a ModuleRelease whose rendered ConfigMap `foo` has been manually modified on the cluster
- **WHEN** the controller runs Phase 4 (Plan Actions)
- **THEN** the `Drifted` condition is set to `True` with reason `DriftDetected`
- **AND** the condition message indicates the number of drifted resources

### Requirement: Drift detection is informational only
Drift detection MUST NOT trigger automatic correction in v1alpha1. Creating a rendered object that does not exist ("A missing object is restored") is not a correction of drift: it never applies an object that exists.

#### Scenario: Drifted resources are not re-applied
- **GIVEN** a ModuleRelease with detected drift and unchanged digests (no-op)
- **AND** no restorable rendered object is missing from the cluster
- **WHEN** the controller completes Phase 4
- **THEN** Phase 5 (Apply) is skipped (no-op behavior preserved)
- **AND** `Drifted=True` condition remains set
- **AND** `Ready=True` is preserved (drift is not a failure)

#### Scenario: A restore leaves drifted resources alone
- **GIVEN** a ModuleInstance with detected drift, unchanged digests and one rendered object missing
- **WHEN** the controller completes Phase 5 (Apply)
- **THEN** only the missing object was applied
- **AND** `Drifted=True` condition remains set

### Requirement: Drift condition cleared after apply
When apply runs (due to source, config or render changes), the controller SHALL clear the `Drifted` condition, since the apply resolves the drift.

#### Scenario: Apply clears drift condition
- **GIVEN** a ModuleRelease with `Drifted=True` and new source changes triggering apply
- **WHEN** Phase 5 (Apply) completes successfully
- **THEN** the `Drifted` condition is removed or set to `False`

### Requirement: Drift runs on no-op reconciles
Drift detection MUST run on every reconcile that renders, even when digest comparison indicates no-op. A reconcile that skips its render because its inputs are unchanged (`render-input-key`) has no rendered objects to compare and MUST NOT run drift detection; it leaves the `Drifted` condition and the drift counter as they were. The drift render interval (`--drift-render-interval`) bounds how long such skips last, so drift detection runs at most once per interval for an object whose inputs do not change, on the first reconcile after the interval.

#### Scenario: Drift detected during no-op
- **GIVEN** a ModuleRelease where source, config, and render digests are unchanged
- **AND** a resource has been manually modified on the cluster
- **AND** the reconcile renders (an input changed or the drift render interval passed)
- **WHEN** the controller reconciles
- **THEN** drift is detected and `Drifted=True` is set
- **AND** apply is still skipped (no source/config/render changes)

#### Scenario: A skipped render runs no drift detection
- **GIVEN** a Ready ModuleInstance whose inputs are unchanged and whose last confirming render is younger than the drift render interval
- **AND** a resource has been manually modified on the cluster
- **WHEN** the controller reconciles
- **THEN** no dry-run is sent and the `Drifted` condition is unchanged

#### Scenario: Drift is found once the interval has passed
- **GIVEN** the same instance, reconciled again after the drift render interval
- **WHEN** the controller reconciles
- **THEN** it renders, drift is detected and `Drifted=True` is set

### Requirement: Drift detection failure increments counter
If the SSA dry-run API call fails, or drift detection cannot run because the effective ServiceAccount cannot be impersonated, the controller MUST increment `status.failureCounters.drift`. A failure other than a `Forbidden` answer or a failed impersonation leaves the `Drifted` condition as it was; those two set it to `Unknown` ("A drift check that is refused or has no identity is reported").

#### Scenario: Dry-run API failure
- **GIVEN** a ModuleRelease where the API server returns an error other than `Forbidden` during dry-run
- **WHEN** the controller runs Phase 4
- **THEN** `status.failureCounters.drift` is incremented
- **AND** the `Drifted` condition keeps its previous value
- **AND** the reconcile continues to Phase 5 (drift failure is non-blocking)

#### Scenario: Dry-run refused
- **GIVEN** a ModuleInstance where the API server answers `Forbidden` to the dry-run
- **WHEN** the controller runs Phase 4
- **THEN** `status.failureCounters.drift` is incremented
- **AND** `Drifted` is `Unknown` with reason `DriftCheckForbidden`
- **AND** the reconcile continues (drift failure is non-blocking)

### Requirement: A withheld resource is excluded from drift detection

Drift detection SHALL skip a resource the reconcile withheld from apply, and its difference from live state SHALL NOT set the `Drifted` condition.

Drift reports that the cluster diverged from what the operator asserts. A withheld resource is one the operator is deliberately not asserting, so reporting it as drift would name a difference the operator created on purpose and intends not to close — a condition that never clears, and one that would bury real drift on the same instance behind it. The refusal that withheld the resource carries that signal instead.

#### Scenario: A withheld resource does not set Drifted

- **WHEN** a reconcile withholds a resource whose live state differs from the rendered one
- **THEN** the `Drifted` condition is not set by that difference

#### Scenario: Real drift is still reported alongside

- **WHEN** an instance has both a withheld resource and another resource that genuinely drifted
- **THEN** the `Drifted` condition is set, reflecting only the genuinely drifted resource

#### Scenario: Drift returns when the resource stops being withheld

- **WHEN** a previously withheld resource is applied on a later reconcile
- **THEN** it is included in drift detection from that point on

### Requirement: A missing object is restored

An object that the render produces and that does not exist on the cluster is not drift, and the `Drifted` condition SHALL NOT report it. When a ModuleInstance reconcile renders, finds every digest unchanged, withholds nothing, and its dry-run shows that one or more rendered objects do not exist, the controller SHALL apply those missing objects, and only those, through the identity that applies the instance. Objects that exist SHALL NOT be applied by this step, so the `Drifted` condition that the same reconcile computed stays as computed.

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

### Requirement: Drift detection runs as the identity that applies

The dry-run of drift detection for a ModuleInstance SHALL be sent through the same client as the apply of that instance: as the ServiceAccount named by `spec.serviceAccountName`, else as the ServiceAccount named by the manager's `--default-service-account` in the instance's namespace, else as the controller itself. The controller SHALL NOT send the dry-run under its own identity when a ServiceAccount is effective, also not when that ServiceAccount cannot be impersonated.

#### Scenario: Dry-run as the named ServiceAccount

- **GIVEN** a Ready ModuleInstance in namespace `team-a` with `spec.serviceAccountName=deploy-sa`
- **WHEN** the controller reconciles and renders
- **THEN** the dry-run request carries `Impersonate-User: system:serviceaccount:team-a:deploy-sa`
- **AND** drift on an object `deploy-sa` may patch is reported as `Drifted=True`

#### Scenario: Dry-run as the flag-defaulted ServiceAccount

- **GIVEN** a Ready ModuleInstance in namespace `team-a` with `spec.serviceAccountName` empty
- **AND** the manager started with `--default-service-account=opm-deployer`
- **WHEN** the controller reconciles and renders
- **THEN** the dry-run request carries `Impersonate-User: system:serviceaccount:team-a:opm-deployer`

#### Scenario: Dry-run as the controller

- **GIVEN** a ModuleInstance with `spec.serviceAccountName` empty and no `--default-service-account`
- **WHEN** the controller reconciles and renders
- **THEN** the dry-run is sent with the controller's own identity and no impersonation header

### Requirement: A drift check that is refused or has no identity is reported

The controller SHALL read each object through the identity of the dry-run before it sends the dry-run. When the API server refuses that read or the dry-run as `Forbidden`, the controller SHALL set the `Drifted` condition to `Unknown` with reason `DriftCheckForbidden` and a message that carries the API server's refusal, which names the identity. When the effective ServiceAccount cannot be impersonated (it does not exist, or the client cannot be built), drift detection SHALL NOT run and the controller SHALL set `Drifted` to `Unknown` with reason `ImpersonationFailed`.

Both cases SHALL count as a failed drift detection, SHALL leave the missing set unknown so that nothing is restored, and SHALL NOT change the `Ready` condition or the outcome of a reconcile with unchanged digests. A later drift detection that succeeds SHALL replace the condition with its verdict, and a successful apply of the rendered set SHALL remove it.

#### Scenario: The ServiceAccount may not dry-run an object

- **GIVEN** a Ready ModuleInstance whose ServiceAccount `deploy-sa` lost the `patch` verb on ConfigMaps
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** `Drifted` is `Unknown` with reason `DriftCheckForbidden` and the message names `system:serviceaccount:<namespace>:deploy-sa`
- **AND** `status.failureCounters.drift` is incremented
- **AND** `Ready=True` is preserved and the outcome is `NoOp`

#### Scenario: The ServiceAccount may not read an object

- **GIVEN** a Ready ModuleInstance whose ServiceAccount `deploy-sa` keeps `patch` and lost the `get` verb on ConfigMaps
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** `Drifted` is `Unknown` with reason `DriftCheckForbidden` and the message names the refused `get`
- **AND** `Drifted` is not `True`: no verdict is given against an object that was not read

#### Scenario: The refusal ends

- **GIVEN** the same instance after `deploy-sa` was given the `patch` verb again
- **WHEN** the controller reconciles and renders
- **THEN** the `Drifted` condition carries the verdict of the dry-run: removed when nothing drifted, `True` when an object did
- **AND** `status.failureCounters.drift` is zero

#### Scenario: The ServiceAccount does not exist

- **GIVEN** a Ready ModuleInstance whose ServiceAccount was deleted
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** no dry-run is sent, by any identity
- **AND** `Drifted` is `Unknown` with reason `ImpersonationFailed`
- **AND** `status.failureCounters.drift` is incremented and `Ready=True` is preserved
