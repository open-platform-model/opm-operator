## ADDED Requirements

### Requirement: Reconcile outcome counter
The controller MUST expose a `opm_controller_reconcile_total` counter metric labeled by outcome.

#### Scenario: Successful reconcile increments counter
- **GIVEN** a ModuleRelease that reconciles successfully with outcome `Applied`
- **WHEN** the reconcile completes
- **THEN** `opm_controller_reconcile_total{outcome="applied"}` is incremented

#### Scenario: Failed reconcile increments counter
- **GIVEN** a ModuleRelease that fails with a transient error
- **WHEN** the reconcile completes
- **THEN** `opm_controller_reconcile_total{outcome="failed_transient"}` is incremented

### Requirement: Reconcile duration histogram
The controller MUST expose a `opm_controller_reconcile_duration_seconds` histogram metric.

#### Scenario: Duration recorded
- **GIVEN** a ModuleRelease reconcile that takes 2.5 seconds
- **WHEN** the reconcile completes
- **THEN** 2.5 is observed in `opm_controller_reconcile_duration_seconds`

### Requirement: Apply resource counter
The controller MUST expose a `opm_controller_apply_resources_total` counter labeled by action (created, updated, unchanged).

#### Scenario: Apply counts recorded
- **GIVEN** a reconcile that creates 3 resources and updates 2
- **WHEN** Phase 5 completes
- **THEN** `opm_controller_apply_resources_total{action="created"}` increments by 3
- **AND** `opm_controller_apply_resources_total{action="updated"}` increments by 2

### Requirement: Prune resource counter
The controller MUST expose a `opm_controller_prune_resources_total` counter.

#### Scenario: Prune count recorded
- **GIVEN** a reconcile that prunes 2 stale resources
- **WHEN** Phase 6 completes
- **THEN** `opm_controller_prune_resources_total` increments by 2

### Requirement: Inventory size gauge
The controller MUST expose a `opm_controller_inventory_size` gauge reflecting current inventory count.

#### Scenario: Inventory gauge updated
- **GIVEN** a ModuleRelease managing 15 resources
- **WHEN** Phase 7 commits status
- **THEN** `opm_controller_inventory_size` is set to 15

### Requirement: Metrics available at /metrics
All custom metrics MUST be available on the controller-runtime default metrics endpoint.

#### Scenario: Metrics endpoint serves custom metrics
- **GIVEN** a running controller
- **WHEN** `/metrics` is scraped
- **THEN** all `opm_controller_*` metrics are present in the response
