## MODIFIED Requirements

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
