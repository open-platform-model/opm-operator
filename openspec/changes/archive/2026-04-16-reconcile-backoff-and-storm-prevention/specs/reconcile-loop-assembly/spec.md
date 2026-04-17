## MODIFIED Requirements

### Requirement: Outcome classification
The reconciler MUST classify each reconcile attempt as one of: `NoOp`, `Applied`, `AppliedAndPruned`, `FailedTransient`, `FailedStalled`.

#### Scenario: Transient failure requeues with explicit backoff
- **WHEN** the outcome is `FailedTransient`
- **THEN** the controller returns `ctrl.Result{RequeueAfter: backoff}` with nil error, where backoff is computed from `failureCounters.reconcile`

#### Scenario: Stalled failure requeues with safety interval
- **WHEN** the outcome is `FailedStalled`
- **THEN** the controller returns `ctrl.Result{RequeueAfter: 30m}` with nil error

### Requirement: Status always patched
The reconciler MUST patch `ModuleRelease.status` at the end of every reconcile attempt that produces a meaningful state change. The reconciler MUST NOT patch status when the outcome is `NoOp` and no state has changed.

#### Scenario: Status updated on failure
- **WHEN** a phase fails
- **THEN** status conditions, `lastAttempted*` fields, and `nextRetryAt` are updated

#### Scenario: Status skip on no-op
- **WHEN** the outcome is `NoOp`
- **THEN** no status patch is issued (generation predicate prevents the resulting watch event from mattering, and skipping the patch avoids unnecessary API calls)

## REMOVED Requirements

### Requirement: shouldSkipStatusPatch guard
**Reason**: Replaced by `GenerationChangedPredicate` at the informer level, which prevents status-only updates from enqueuing reconciles entirely. The in-reconcile guard is redundant.
**Migration**: Remove `shouldSkipStatusPatch` function from `internal/reconcile/modulerelease.go`. The generation predicate handles this concern at a cheaper layer.
