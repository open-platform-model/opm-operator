## Purpose

Define the condition types and reason constants the operator writes on its
custom resources, and the helpers that set them, so every reconciler reports
Ready, Reconciling, Stalled and source readiness in one shared vocabulary.

## Requirements

### Requirement: Condition type constants
The `internal/status` package MUST define string constants for condition types: `ReadyCondition`, `ReconcilingCondition`, `StalledCondition`, `SourceReadyCondition`.

#### Scenario: Constants available
- **WHEN** code imports `internal/status`
- **THEN** all four condition type constants are available and match the design doc values

### Requirement: Reason constants

The status package SHALL define the reason constants used on the Ready condition, including `SkewRefused` (Ready=False: the platform's skew policy is `Refuse` and the module requires a newer catalog build than the platform pins), `DuplicateIdentities` (Ready=False: two or more rendered objects share one Kubernetes apply identity, so the render is refused before apply naming every producing component; 0015:D15), `ReconcilePanic` (Ready=False, Reconciling=True: a ModuleInstance or ModulePackage reconcile panicked and the attempt was recorded as a failure before the panic propagated) and `RenderTimedOut` (Ready=False, Reconciling=True, not stalled: a ModuleInstance or ModulePackage render did not finish within `--render-timeout`, and the attempt retries on the backoff) beside the existing `ResolutionFailed`, `RenderFailed`, `PlatformNotReady` and the Platform reasons `Generated`, `BuildFailed`, `GenerateFailed`.

#### Scenario: Reason constants available
- **WHEN** code imports `internal/status`
- **THEN** all reason constants are available as exported string constants

#### Scenario: Skew refusal reason is available

- **WHEN** a render is refused under the `Refuse` policy
- **THEN** the reconciler marks `Ready=False` with reason `SkewRefused`

#### Scenario: Duplicate-identity reason is available

- **WHEN** a render is refused because two compiled objects share one apply identity
- **THEN** the reconciler marks `Ready=False` with reason `DuplicateIdentities`

#### Scenario: Reconcile-panic reason is available

- **WHEN** a ModuleInstance or ModulePackage reconcile panics
- **THEN** the reconciler marks `Ready=False` and `Reconciling=True` with reason `ReconcilePanic` and removes `Stalled`

#### Scenario: Render-timeout reason is available

- **WHEN** a ModuleInstance or ModulePackage render does not finish within `--render-timeout`
- **THEN** the reconciler marks `Ready=False` and `Reconciling=True` with reason `RenderTimedOut` and removes `Stalled`

### Requirement: Condition helper functions
The `internal/status` package MUST provide helper functions for common condition transitions.

#### Scenario: Mark reconciling
- **WHEN** `MarkReconciling` is called on a ModuleRelease with a reason and message
- **THEN** the `Reconciling` condition is set to `True` and `Ready` is set to `Unknown`

#### Scenario: Mark stalled
- **WHEN** `MarkStalled` is called on a ModuleRelease with a reason and message
- **THEN** the `Stalled` condition is set to `True` and `Ready` is set to `False`

#### Scenario: Mark ready
- **WHEN** `MarkReady` is called on a ModuleRelease with a message
- **THEN** the `Ready` condition is set to `True` and `Reconciling` and `Stalled` are removed

#### Scenario: Mark source ready
- **WHEN** `MarkSourceReady` is called with artifact revision info
- **THEN** the `SourceReady` condition is set to `True`

#### Scenario: Mark source not ready
- **WHEN** `MarkSourceNotReady` is called with a reason
- **THEN** the `SourceReady` condition is set to `False`

### Requirement: Flux condition interface compliance
`ModuleRelease` MUST implement `conditions.Getter` and `conditions.Setter` interfaces from `fluxcd/pkg/runtime/conditions`.

#### Scenario: Interface satisfaction
- **WHEN** a `*ModuleRelease` is passed to Flux condition helpers
- **THEN** the helpers compile and function correctly

### Requirement: The Healthy condition vocabulary

The `internal/status` package SHALL define the condition type `HealthyCondition` (`"Healthy"`) and the reasons `RolledOut` (`Healthy=True`), `NotRolledOut` (`Healthy=False`), `ProgressDeadlineExceeded` (`Healthy=False`, a Deployment's rollout is stalled) and `HealthUnknown` (`Healthy=Unknown`). It SHALL provide one helper that sets `Healthy` from a status, a reason and a message, and touches no other condition. `Healthy` is independent of `Ready`: no helper that sets `Ready`, `Reconciling` or `Stalled` changes it, except that `MarkManagedExternally` and `MarkSelfManagementRefused` remove it.

#### Scenario: The constants are available

- **WHEN** code imports `internal/status`
- **THEN** `HealthyCondition`, `RolledOutReason`, `NotRolledOutReason`, `ProgressDeadlineExceededReason` and `HealthUnknownReason` are exported string constants with those values

#### Scenario: Marking Ready leaves Healthy alone

- **WHEN** `MarkReady`, `MarkReconciling`, `MarkStalled` or `MarkSuspended` is called on an object carrying `Healthy=False`
- **THEN** `Healthy` is unchanged

#### Scenario: The helper sets only Healthy

- **WHEN** the helper sets `Healthy=True` with reason `RolledOut` on an object carrying `Ready=True`
- **THEN** `Healthy` is `True` with reason `RolledOut` and every other condition is unchanged

### Requirement: The Drifted condition can be Unknown

The status package SHALL define the reason constant `DriftCheckForbidden` and a helper that sets the `Drifted` condition to `Unknown` with a given reason and message. The helper SHALL NOT change the `Ready`, `Reconciling` or `Stalled` conditions. The reasons used with it are `DriftCheckForbidden` (the API server refused the dry-run of drift detection) and `ImpersonationFailed` (the effective ServiceAccount could not be impersonated, so drift detection did not run).

#### Scenario: Mark drift unknown

- **WHEN** the helper is called on a ModuleInstance with `Ready=True`, reason `DriftCheckForbidden` and a message
- **THEN** `Drifted` is `Unknown` with that reason and message
- **AND** `Ready` is still `True`

#### Scenario: A verdict replaces Unknown

- **GIVEN** a ModuleInstance with `Drifted=Unknown`
- **WHEN** `ClearDrifted` is called
- **THEN** the `Drifted` condition is removed

### Requirement: The ClaimConflict reason
The status package SHALL define the reason constant `ClaimConflict`. A ModuleInstance or ModulePackage reconcile whose apply refused the forced recreate of a PersistentVolumeClaim MUST set `Ready=False` with reason `ClaimConflict`. The message MUST name the claim as `<namespace>/<name>`, MUST say that the claim was kept, and MUST name the ways out, `spec.dataPolicy: Delete` among them. When the check before the apply refused the claim, the message MUST also name the refused field and the API server's message and MUST say that nothing was applied. When the resource manager's delete guard kept the claim (the claim changed after the check), no refusal is at hand and a stage of the apply is already under way: the message MUST NOT name a field and MUST NOT say that nothing was applied. The reconcile MUST NOT set `Stalled`: it MUST retry on the bounded backoff, because the conflict can be resolved on the claim, which changes nothing on the reconciled object. The reconcile MUST NOT prune, MUST NOT record a new inventory and MUST NOT record the rendered digests as applied.

#### Scenario: A refused claim on a ModuleInstance
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` and no `spec.dataPolicy`, whose render changes `storageClassName` of a live PersistentVolumeClaim `media/config`
- **WHEN** the controller reconciles
- **THEN** `Ready` is `False` with reason `ClaimConflict` and a message that contains `media/config` and `spec`
- **AND** `Stalled` is not set, and the result asks for a retry after a backoff
- **AND** the claim has the UID it had
- **AND** no other object of the render was changed

#### Scenario: The conflict clears when the render fits the claim again
- **GIVEN** that ModuleInstance with reason `ClaimConflict`
- **WHEN** the render goes back to the claim's `storageClassName` and the controller reconciles
- **THEN** `Ready` is `True`

#### Scenario: Delete recreates the claim
- **GIVEN** a ModuleInstance with `spec.rollout.forceConflicts: true` and `spec.dataPolicy: Delete`, whose render changes `storageClassName` of a live PersistentVolumeClaim
- **WHEN** the controller reconciles
- **THEN** the claim is deleted and created again with the new `storageClassName`, and `Ready` is `True`

#### Scenario: A claim kept by the delete guard
- **GIVEN** an apply whose check passed and whose staged apply then reached for the delete of the PersistentVolumeClaim `media/config`
- **WHEN** the controller reports the reconcile
- **THEN** `Ready` is `False` with reason `ClaimConflict` and a message that contains `media/config` and `spec.dataPolicy`
- **AND** the message names no refused field and does not say that nothing was applied
