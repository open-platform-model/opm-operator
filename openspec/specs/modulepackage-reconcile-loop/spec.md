## Purpose

Defines the end-to-end reconciliation loop for the Release CRD: phase ordering, triggers, suspend/no-op handling, finalizer cleanup, and status shape.

## Requirements

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

### Requirement: Reconcile triggers
The `ReleaseReconciler` MUST reconcile on three triggers: CR spec changes, source artifact revision changes, and interval-based re-reconciliation. An interval requeue renders only when a render input key part changed, the resolved source differs from `status.source`, the package is not `Ready`, or `--drift-render-interval` has passed since `status.lastAppliedInputs.renderedAt` (`render-input-key`); otherwise it resolves the source and requeues without rendering.

#### Scenario: CR spec change triggers reconcile
- **WHEN** a Release CR's spec is modified (path, sourceRef, prune, etc.)
- **THEN** reconciliation is triggered immediately

#### Scenario: Source revision change triggers reconcile
- **WHEN** the referenced Flux source's `status.artifact` changes (new revision/digest)
- **THEN** all Release CRs referencing that source are enqueued for reconciliation

#### Scenario: Interval-based re-reconciliation
- **WHEN** the interval period elapses since the last successful reconcile
- **THEN** reconciliation is triggered, and it renders and re-applies if needed when an input changed or the drift render interval has passed since `status.lastAppliedInputs.renderedAt`

### Requirement: Suspend check
The `ReleaseReconciler` MUST skip reconciliation when `spec.suspend` is true.

#### Scenario: Suspended release
- **WHEN** `spec.suspend` is true
- **THEN** the controller sets condition reason `Suspended` and returns without requeue

#### Scenario: Resume from suspend
- **WHEN** `spec.suspend` changes from true to false
- **THEN** the controller emits a resume event and proceeds with normal reconciliation

### Requirement: No-op detection
The `ReleaseReconciler` MUST detect no-op reconciliations when source artifact revision, config, render, and inventory digests all match the last applied values.

#### Scenario: All digests match
- **WHEN** source artifact digest, config digest, render digest, and inventory digest all match the last applied values
- **THEN** the controller skips apply and prune, keeps `Ready=True`, and requeues with interval

### Requirement: Source digest from artifact metadata
The Release reconciler MUST derive the source digest from the Flux source artifact's revision and digest, not from CUE module path/version.

#### Scenario: Source digest computation
- **WHEN** the Flux source artifact has revision `main@sha1:abc123` and digest `sha256:def456`
- **THEN** the source digest is computed from the artifact revision and digest

### Requirement: Finalizer and deletion cleanup
The `ReleaseReconciler` MUST register a finalizer on Release CRs and clean up owned resources on deletion.

#### Scenario: Deletion with prune enabled
- **WHEN** a Release CR is deleted and `spec.prune` is true
- **THEN** the controller prunes all inventory entries, then removes the finalizer

#### Scenario: Deletion with prune disabled
- **WHEN** a Release CR is deleted and `spec.prune` is false
- **THEN** the controller removes the finalizer without pruning (orphans resources)

### Requirement: Status always patched
The `ReleaseReconciler` MUST patch `Release.status` at the end of every reconcile attempt, including NoOp. The status shape mirrors ModuleRelease: conditions, digests, inventory, history, failure counters, `nextRetryAt`, and `lastAppliedInputs`. A reconcile that skips its render because its inputs are unchanged (`render-input-key`) is not an attempt and MUST NOT patch status; it requeues after `spec.interval`.

#### Scenario: Status updated on failure
- **WHEN** a phase fails
- **THEN** status conditions, `lastAttempted*` fields, `failureCounters`, and `nextRetryAt` are updated

#### Scenario: Successful reconcile status
- **WHEN** all phases succeed
- **THEN** `Ready=True`, `lastApplied*` digests are set, inventory is replaced, and a success history entry is recorded

#### Scenario: A skipped interval patches nothing
- **WHEN** the interval requeue finds the package's inputs unchanged within the drift render interval
- **THEN** no status patch is sent and the reconcile requeues after `spec.interval`

### Requirement: Source status tracking
The `ReleaseStatus` MUST include a `source` field reflecting the resolved Flux artifact metadata (ref, revision, digest, URL).

#### Scenario: Source metadata recorded
- **WHEN** a Flux source is resolved successfully
- **THEN** `status.source` reflects the source reference, artifact revision, artifact digest, and artifact URL
