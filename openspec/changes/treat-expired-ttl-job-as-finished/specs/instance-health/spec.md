## ADDED Requirements

### Requirement: An expired Job does not make a ModuleInstance unhealthy

A `batch/v1` Job that a ModuleInstance renders with `ttlSecondsAfterFinished` and that the cluster removed SHALL NOT make `Healthy` `False`. The reconcile that finds it absent with unchanged digests removes it from `status.inventory` (`drift-detection`, "A missing object is restored"), and `Healthy` SHALL be judged over the entries that remain. The ready count and the total in the message SHALL count those entries only. A reconcile that would skip its render SHALL NOT report an absent Job as `Missing` before a render classified it, unless the last drift detection failed (`render-input-key`).

A Job without a TTL that does not exist, and every other missing object, SHALL be judged as before. This requirement does not apply to a ModulePackage.

#### Scenario: An instance with an expired Job stays healthy

- **GIVEN** a ModuleInstance with `Healthy=True` whose inventory holds a ConfigMap and a Job with a TTL that completed
- **WHEN** the cluster removes the Job and the instance is reconciled on its interval
- **THEN** `Healthy` stays `True` with reason `RolledOut` and the message counts the ConfigMap only
- **AND** the reconcile requeues on the instance reconcile interval, not on the health requeue

#### Scenario: A missing Job without a TTL after a failed drift detection

- **GIVEN** a ModuleInstance whose Job without a TTL was deleted
- **WHEN** drift detection fails on the reconcile that renders for it
- **THEN** `Healthy` is `False` with reason `NotRolledOut` and names the Job as `Missing`
