## MODIFIED Requirements

### Requirement: End-to-end release scenarios

The synthesis flow MUST behave predictably across the common user-facing scenarios.

#### Scenario: Happy path
- **WHEN** a user creates a `ModuleRelease` CR with valid `spec.module.path` and `spec.module.version`
- **THEN** the controller synthesizes the `#ModuleRelease` CUE package, CUE resolves the module from the OCI registry, evaluation produces concrete `components`, the render pipeline generates Kubernetes resources, resources are applied via SSA, and `status.conditions` reports `Ready=True`

#### Scenario: Module not found in registry
- **WHEN** a user creates a `ModuleRelease` CR with a `spec.module.path` that does not exist in the registry
- **THEN** acquisition fails with a registry fetch failure of kind not found, `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and no `Stalled` condition, and the controller retries on the exponential backoff capped at 5 minutes until the path resolves or the CR changes

#### Scenario: Invalid values
- **WHEN** a user creates a `ModuleRelease` CR with values that conflict with `#config`
- **THEN** the controller acquires the module, the values are refused against the module's `#config` with an error naming their positions in `spec.values`, and `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True`

#### Scenario: Version upgrade
- **WHEN** a user updates `spec.module.version` on an existing `ModuleRelease` CR
- **THEN** the controller detects the CR change, re-synthesizes the package with the new version, CUE resolves the new version from the registry, new resources are rendered and applied, and previous resources no longer in the inventory are pruned when `prune: true`
