## MODIFIED Requirements

### Requirement: Finalizer registration
The controller MUST add the finalizer `releases.opmodel.dev/cleanup` to a ModuleRelease during Phase 0 if it is not already present. The reconcile call that adds the finalizer MUST request an immediate requeue (`ctrl.Result{Requeue: true}`) rather than relying on the watch event produced by the finalizer patch — that watch event is filtered by the controller's `predicate.GenerationChangedPredicate`, because finalizer patches modify `metadata.finalizers` but do not bump `metadata.generation`. Without an explicit requeue, the next reconcile would be deferred to the periodic resync (default 10h), leaving freshly-created ModuleReleases without status conditions for an unacceptable duration.

#### Scenario: First reconcile adds finalizer
- **GIVEN** a ModuleRelease without the `releases.opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles the resource
- **THEN** the finalizer is added to `metadata.finalizers`

#### Scenario: Subsequent reconciles preserve finalizer
- **GIVEN** a ModuleRelease that already has the `releases.opmodel.dev/cleanup` finalizer
- **WHEN** the controller reconciles the resource
- **THEN** the finalizer remains unchanged

#### Scenario: Finalizer-add reconcile requests immediate requeue
- **GIVEN** a freshly-created ModuleRelease without the `releases.opmodel.dev/cleanup` finalizer
- **WHEN** the controller's first `Reconcile` call adds the finalizer
- **THEN** the reconcile returns a result with `Requeue=true` so the workqueue re-enqueues the request directly (bypassing the predicate)
- **AND** the next reconcile starts within the rate limiter's normal cadence (typically under one second), not the periodic resync window

#### Scenario: Status conditions appear after creation without manual triggers
- **GIVEN** a ModuleRelease created via the API server
- **WHEN** the controller observes the create event and reconciles via the manager
- **THEN** within a small bounded window (e.g., 10 seconds in envtest, single-digit seconds in production) `status.conditions` contains at least one of `Ready`, `Reconciling`, or `Stalled`
- **AND** no manual `kubectl edit`, periodic-resync, or other external trigger is required to produce these conditions
