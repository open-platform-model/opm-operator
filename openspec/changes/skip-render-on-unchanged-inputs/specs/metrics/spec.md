## ADDED Requirements

### Requirement: Skipped render counter
The controller MUST expose an `opm_controller_render_skipped_total` counter labelled by `kind` (`ModuleInstance` or `ModulePackage`), `name` and `namespace`, incremented once for each reconcile that skips its render because its inputs are unchanged. A skipped reconcile is not an attempt and MUST NOT increment `opm_controller_reconcile_total`.

#### Scenario: A skipped render is counted
- **GIVEN** a ModuleInstance `web` in namespace `apps` whose inputs are unchanged within the drift render interval
- **WHEN** it is reconciled
- **THEN** `opm_controller_render_skipped_total{kind="ModuleInstance",name="web",namespace="apps"}` is incremented
- **AND** `opm_controller_reconcile_total` is not incremented
