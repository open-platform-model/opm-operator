## ADDED Requirements

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
Drift detection MUST NOT trigger automatic correction in v1alpha1.

#### Scenario: Drifted resources are not re-applied
- **GIVEN** a ModuleRelease with detected drift and unchanged digests (no-op)
- **WHEN** the controller completes Phase 4
- **THEN** Phase 5 (Apply) is skipped (no-op behavior preserved)
- **AND** `Drifted=True` condition remains set
- **AND** `Ready=True` is preserved (drift is not a failure)

### Requirement: Drift condition cleared after apply
When apply runs (due to source/config/render changes), drift is resolved.

#### Scenario: Apply clears drift condition
- **GIVEN** a ModuleRelease with `Drifted=True` and new source changes triggering apply
- **WHEN** Phase 5 (Apply) completes successfully
- **THEN** the `Drifted` condition is removed or set to `False`

### Requirement: Drift runs on no-op reconciles
Drift detection MUST run even when digest comparison indicates no-op.

#### Scenario: Drift detected during no-op
- **GIVEN** a ModuleRelease where source, config, and render digests are unchanged
- **AND** a resource has been manually modified on the cluster
- **WHEN** the controller reconciles
- **THEN** drift is detected and `Drifted=True` is set
- **AND** apply is still skipped (no source/config/render changes)

### Requirement: Drift detection failure increments counter
If the SSA dry-run API call fails, the controller MUST increment `status.failureCounters.drift`.

#### Scenario: Dry-run API failure
- **GIVEN** a ModuleRelease where the API server returns an error during dry-run
- **WHEN** the controller runs Phase 4
- **THEN** `status.failureCounters.drift` is incremented
- **AND** the `Drifted` condition is not set (unknown state)
- **AND** the reconcile continues to Phase 5 (drift failure is non-blocking)
