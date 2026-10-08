## MODIFIED Requirements

### Requirement: A reconcile requeues until the instance has rolled out

When `Healthy` is `False` with reason `NotRolledOut`, or `Unknown` because a read failed or the reader could not be built, the reconcile SHALL requeue after half the time since `status.lastAppliedAt`, at least 5 seconds and at most 2 minutes (5 seconds when `lastAppliedAt` is unset or in the future). When `Healthy` is `False` with reason `ProgressDeadlineExceeded`, the reconcile SHALL stop the fast requeue and requeue after the stalled recheck interval (30 minutes). When `Healthy` is `True`, or `Unknown` because the inventory is empty, health SHALL NOT cause a requeue: a ModuleInstance then requeues on the instance reconcile interval alone (`reconcile-loop-assembly`), and not at all when that interval is `0`. A ModulePackage SHALL requeue after the shorter of the health requeue and `spec.interval`. A health requeue SHALL NOT set `status.nextRetryAt` and SHALL NOT count as a failure.

#### Scenario: A fresh rollout is checked quickly

- **WHEN** an instance was applied 4 seconds ago and is `NotRolledOut`
- **THEN** the reconcile requeues after 5 seconds and `status.nextRetryAt` is unset

#### Scenario: A long rollout settles at the ceiling

- **WHEN** an instance applied 10 minutes ago is still `NotRolledOut`
- **THEN** the reconcile requeues after 2 minutes

#### Scenario: A stalled rollout stops the fast requeue

- **WHEN** `Healthy` becomes `False` with reason `ProgressDeadlineExceeded`
- **THEN** the reconcile requeues after 30 minutes

#### Scenario: A rolled-out instance is not requeued for health

- **WHEN** a ModuleInstance's reconcile ends `RolledOut`
- **THEN** health adds no requeue, and the reconcile requeues on the instance reconcile interval only
- **AND** it returns without a requeue when that interval is `0`

#### Scenario: A package keeps its interval as an upper bound

- **WHEN** a ModulePackage with `spec.interval: 1m` is `NotRolledOut` and was applied 10 minutes ago
- **THEN** it requeues after 1 minute
