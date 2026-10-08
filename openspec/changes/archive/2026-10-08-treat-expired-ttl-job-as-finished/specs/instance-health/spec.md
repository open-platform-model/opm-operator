## ADDED Requirements

### Requirement: An expired Job does not make a ModuleInstance unhealthy

A `batch/v1` Job that a ModuleInstance renders with `ttlSecondsAfterFinished` and that the cluster removed SHALL NOT make `Healthy` `False`. The reconcile that finds it absent with unchanged digests removes it from `status.inventory` (`drift-detection`, "A missing object is restored"), and `Healthy` SHALL be judged over the entries that remain. The ready count and the total in the message SHALL count those entries only. A reconcile that would skip its render SHALL NOT report an absent Job as `Missing` before a render classified it, unless the last drift detection failed (`render-input-key`).

The operator does not know how the Job ended. A Job with a TTL that failed makes `Healthy` `False` while it exists; once the cluster removed it, it is an expired Job like any other and no longer counts. When the removal leaves `status.inventory` with no entries, `Healthy` SHALL be `Unknown` with reason `HealthUnknown`, as for any empty inventory.

A Job without a TTL that does not exist, and every other missing object, SHALL be judged as before. This requirement does not apply to a ModulePackage.

#### Scenario: An instance with an expired Job stays healthy

- **GIVEN** a ModuleInstance with `Healthy=True` whose inventory holds a ConfigMap and a Job with a TTL that completed
- **WHEN** the cluster removes the Job and the instance is reconciled on its interval
- **THEN** `Healthy` stays `True` with reason `RolledOut` and the message counts the ConfigMap only
- **AND** the reconcile requeues on the instance reconcile interval, not on the health requeue

#### Scenario: The only inventory object was an expired Job

- **GIVEN** a ModuleInstance whose render is one Job with a TTL
- **WHEN** the cluster removes the Job and the controller reconciles with unchanged digests
- **THEN** `status.inventory.entries` is empty and `Healthy` is `Unknown` with reason `HealthUnknown`
- **AND** the reconcile requeues on the instance reconcile interval

#### Scenario: An absent Job after a failed drift detection

- **GIVEN** a ModuleInstance whose inventory Job does not exist
- **WHEN** drift detection fails on the reconcile that renders for it
- **THEN** the Job stays in `status.inventory`, and `Healthy` is `False` with reason `NotRolledOut` and names the Job as `Missing`
