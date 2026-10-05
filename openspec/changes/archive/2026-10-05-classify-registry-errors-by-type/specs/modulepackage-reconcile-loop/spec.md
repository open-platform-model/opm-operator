## MODIFIED Requirements

### Requirement: Full reconcile loop execution
The `ReleaseReconciler` MUST execute phases sequentially: source resolution → artifact fetch → path navigation → CUE load → kind detection → render → apply → prune → status update. A failure in CUE load or render MUST be classified by its type (see `reconcile-backoff`, "Registry fetch failures are transient wherever they occur"): a typed registry fetch failure retries on the backoff, any other failure stalls.

#### Scenario: First successful reconcile (ModuleRelease)
- **WHEN** a Release CR is created with a valid `sourceRef`, the Flux source is ready, `spec.path` contains a valid `release.cue` evaluating to `#ModuleRelease`
- **THEN** the controller resolves the source, fetches the artifact, navigates to path, loads CUE, detects kind, renders resources, applies via SSA, updates status with conditions/digests/inventory/history, and sets `Ready=True`

#### Scenario: Source not ready
- **WHEN** the referenced Flux source exists but is not ready
- **THEN** the controller sets `Ready=False` with reason `SourceNotReady` and requeues with interval

#### Scenario: Render failure
- **WHEN** the CUE package loads but its render fails evaluation for a cause that is neither a resolution-class failure nor a registry fetch failure
- **THEN** the controller sets `Ready=False`, `Stalled=True` with reason `RenderFailed`, and does NOT modify inventory or attempt apply

#### Scenario: Package load failure
- **WHEN** the CUE package fails to load for a cause that is not a registry fetch failure, for example a CUE syntax error
- **THEN** the controller sets `Ready=False`, `Stalled=True` with reason `ResolutionFailed`, requeues on the 30-minute recheck, and does NOT modify inventory or attempt apply

#### Scenario: Registry failure during load or render
- **WHEN** loading or rendering the CUE package fails with a typed registry fetch failure
- **THEN** the controller sets `Ready=False` with reason `ResolutionFailed`, does not set `Stalled=True`, requeues on the exponential backoff, and does NOT modify inventory or attempt apply

#### Scenario: Apply failure
- **WHEN** SSA apply fails
- **THEN** the controller sets `Ready=False` with reason `ApplyFailed`, does NOT prune, does NOT update `lastApplied*` digests, and requeues with backoff
