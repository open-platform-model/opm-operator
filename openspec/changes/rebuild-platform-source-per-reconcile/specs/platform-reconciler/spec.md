## MODIFIED Requirements

### Requirement: Build failures requeue on a bounded interval

When closure derivation, generation or the build fails, the `PlatformReconciler` SHALL requeue the `Platform` after a bounded interval rather than waiting for a spec change; no such failure is terminal. The reconciler SHALL set the failure reason (`BuildFailed` or `GenerateFailed`) and SHALL preserve any previously recorded good module. Each reconcile SHALL resolve module files for closure derivation through a module-file source built for that reconcile, so that no module-file lookup failure met by an earlier reconcile's closure derivation decides a later reconcile's closure derivation. A module-file source injected into the reconciler (tests) SHALL be used as given.

#### Scenario: Failure requeues instead of stalling indefinitely

- **WHEN** the build fails for the `cluster` Platform
- **THEN** the reconcile result carries a non-zero `RequeueAfter`, the status is `Ready=False`, and the last-good record, if any, is still held

#### Scenario: Recovery without a spec change

- **WHEN** a Platform is in `BuildFailed` and the registry condition clears with no change to the spec
- **THEN** a subsequent automatic reconcile builds successfully and sets `Ready=True` (reason `Generated`)

#### Scenario: A transient registry failure does not outlive its reconcile

- **WHEN** a reconcile of the `cluster` Platform ends `Ready=False` with reason `BuildFailed` because the registry refused the catalog's module file, and the registry then serves it again, with no change to the Platform spec, the reconciler's configuration or the operator process
- **THEN** the next reconcile fetches the module file from the registry, builds successfully, and sets `Ready=True` (reason `Generated`) at the same generation
