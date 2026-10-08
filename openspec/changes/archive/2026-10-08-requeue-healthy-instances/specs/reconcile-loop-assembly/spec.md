## ADDED Requirements

### Requirement: A healthy ModuleInstance is reconciled periodically

The operator SHALL take an instance reconcile interval from the flag `--instance-reconcile-interval` (default 10 minutes). When a ModuleInstance reconcile ends with outcome `NoOp`, `Applied` or `AppliedAndPruned`, or skips its render because its inputs are unchanged, and the health judgement asks for no requeue, the reconcile SHALL requeue after the interval plus a random addition of at most 10 percent of it. When the health judgement asks for a requeue, that requeue SHALL apply unchanged. The periodic requeue SHALL NOT set `status.nextRetryAt` and SHALL NOT count as a failure.

An interval of `0` SHALL disable the periodic requeue. The operator MUST refuse to start with a negative interval. The interval is operator-wide: the ModuleInstance API SHALL NOT gain a field for it.

A suspended ModuleInstance, a CLI-owned ModuleInstance and the operator's own instance SHALL NOT be requeued by the interval. A failed reconcile SHALL keep the requeue of its failure class.

The periodic reconcile of an instance whose inputs and objects are unchanged SHALL end as a skipped render or as a `NoOp`: it sends no apply, records no history entry and changes no condition.

#### Scenario: A rolled-out instance requeues on the interval

- **WHEN** an operator-owned ModuleInstance reconcile ends `Applied` with `Healthy=True` and the interval is 10 minutes
- **THEN** the reconcile requeues after at least 10 and at most 11 minutes
- **AND** `status.nextRetryAt` is unset

#### Scenario: A NoOp and a skipped render requeue on the interval

- **WHEN** a later reconcile of the same unchanged instance ends as a `NoOp` or skips its render
- **THEN** it requeues after at least 10 and at most 11 minutes

#### Scenario: A skipped periodic reconcile writes nothing

- **WHEN** the periodic reconcile of an unchanged, rolled-out instance skips its render
- **THEN** no patch and no status patch is sent for the instance

#### Scenario: A suspended instance is not requeued

- **WHEN** a ModuleInstance has `spec.suspend: true` and the interval is 10 minutes
- **THEN** its reconcile returns without a requeue

#### Scenario: A CLI-owned instance is not requeued

- **WHEN** a ModuleInstance has `spec.owner: cli` and the interval is 10 minutes
- **THEN** its reconcile returns without a requeue

#### Scenario: The interval is disabled

- **WHEN** the interval is `0` and a ModuleInstance reconcile ends rolled out
- **THEN** the reconcile returns without a requeue

#### Scenario: A negative interval stops the operator

- **WHEN** the operator starts with `--instance-reconcile-interval=-1s`
- **THEN** it logs the invalid value and exits with a non-zero status

## MODIFIED Requirements

### Requirement: No-op detection
The reconciler MUST detect no-op reconciliations and skip apply/prune when nothing changed. A reconcile is a no-op only when every digest matches and no restorable rendered object is missing from the cluster (`drift-detection`, "A missing object is restored").

#### Scenario: All digests match
- **WHEN** source, config, render, and inventory digests all match the last applied values
- **AND** no restorable rendered object is missing from the cluster
- **THEN** the controller skips apply and prune, keeps `Ready=True`, and does not record a new history entry

#### Scenario: All digests match and an object is missing
- **WHEN** all four digests match the last applied values and a restorable rendered object does not exist on the cluster
- **THEN** the controller applies the missing object, prunes nothing, and records a history entry
