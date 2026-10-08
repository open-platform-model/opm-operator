## ADDED Requirements

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

The controller SHALL read each object through the identity of the dry-run before it sends the dry-run. When the API server refuses that read or the dry-run as `Forbidden`, the controller SHALL set the `Drifted` condition to `Unknown` with reason `DriftCheckForbidden` and a message that carries the API server's refusal, which names the identity. `Forbidden` is every 403 answer, not only a missing RBAC verb of the identity: an admission control that denies the dry-run with 403, and a controller that may not impersonate the ServiceAccount, give the same reason, and the message says which. When the effective ServiceAccount cannot be impersonated (it does not exist, or the client cannot be built), drift detection SHALL NOT run and the controller SHALL set `Drifted` to `Unknown` with reason `ImpersonationFailed`.

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

#### Scenario: The ServiceAccount may not create a missing object

- **GIVEN** a Ready ModuleInstance whose ServiceAccount `deploy-sa` lost the `create` verb on ConfigMaps, and whose rendered ConfigMap was deleted
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** `Drifted` is `Unknown` with reason `DriftCheckForbidden`
- **AND** the ConfigMap is not created, by any identity

#### Scenario: A restore runs as the ServiceAccount

- **GIVEN** a Ready ModuleInstance with `spec.serviceAccountName=deploy-sa` whose rendered ConfigMap was deleted
- **WHEN** the controller reconciles and renders with unchanged digests
- **THEN** every read, dry-run and apply of the ConfigMap carries the identity of `deploy-sa`
- **AND** the ConfigMap exists again

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

## MODIFIED Requirements

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
