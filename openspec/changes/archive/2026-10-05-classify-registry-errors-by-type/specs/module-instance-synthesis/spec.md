## MODIFIED Requirements

### Requirement: Status reporting

The `status.source` field MAY be updated to reflect:

- The CUE module path and version (from `spec.module`).
- Whether module resolution from the registry succeeded.

The `status.conditions` MUST report:

- `Ready=True` when the module is successfully resolved, rendered, and applied.
- `Ready=False` with reason `ResolutionFailed` when the module cannot be resolved
  into a usable, trustworthy input for rendering. This covers:
  - The module cannot be acquired from the registry.
  - A registry fetch fails during values compile, instance synthesis or the
    render build.
  - The acquired module's declared identity (module path or version in its
    metadata) disagrees with the coordinate it was fetched by.
  - The acquired artifact is not a module or is structurally invalid.
  - The module demands contracts that the generated platform does not
    provide.
- `Ready=False` with reason `RenderFailed` when synthesis, CUE evaluation or
  rendering fails for a cause that is neither a resolution-class failure nor a
  registry fetch failure.
- `Stalled=True` when the failure is not transient. A typed registry fetch
  failure in any phase is transient: it MUST NOT set `Stalled=True` and retries
  on the exponential backoff capped at 5 minutes (see `reconcile-backoff`,
  "Registry fetch failures are transient wherever they occur"). An acquisition
  failure that is not a registry fetch failure stalls.

#### Scenario: Success reported
- **WHEN** the module resolves, renders, and applies successfully
- **THEN** `status.conditions` reports `Ready=True`

#### Scenario: Resolution failure reported
- **WHEN** the module cannot be acquired from the registry because the fetch failed (the registry is unreachable, does not hold the module, or refuses the credentials)
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and the instance retries on the exponential backoff

#### Scenario: Identity mismatch reported as resolution failure
- **WHEN** the acquired module's declared identity disagrees with the coordinate it was fetched by (mismatched module path or version)
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the mismatch message

#### Scenario: Unresolved platform demands reported as resolution failure
- **WHEN** the module demands contracts the generated platform does not provide — including when that failure is reported together with unmatched-component failures
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`, and a Warning event carries the unresolved-demands message

#### Scenario: Render failure reported
- **WHEN** synthesis, CUE evaluation or rendering fails for a cause that is neither a resolution-class failure nor a registry fetch failure
- **THEN** `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True` when user input must change to resolve the failure

#### Scenario: Registry failure after acquisition reported as transient resolution failure
- **WHEN** instance synthesis or the render build fails because a registry fetch failed
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and the instance retries on the exponential backoff

### Requirement: End-to-end release scenarios

The synthesis flow MUST behave predictably across the common user-facing scenarios.

#### Scenario: Happy path
- **WHEN** a user creates a `ModuleRelease` CR with valid `spec.module.path` and `spec.module.version`
- **THEN** the controller synthesizes the `#ModuleRelease` CUE package, CUE resolves the module from the OCI registry, evaluation produces concrete `components`, the render pipeline generates Kubernetes resources, resources are applied via SSA, and `status.conditions` reports `Ready=True`

#### Scenario: Module not found in registry
- **WHEN** a user creates a `ModuleRelease` CR with a `spec.module.path` that does not exist in the registry
- **THEN** acquisition fails with a registry fetch failure of kind not found, `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and no `Stalled` condition, and the controller retries on the exponential backoff capped at 5 minutes until the path resolves or the CR changes

#### Scenario: Invalid values
- **WHEN** a user creates a `ModuleRelease` CR with values that do not satisfy `#config`
- **THEN** the controller synthesizes the package and CUE resolves the module, value validation fails in `ParseModuleRelease`, and `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True`

#### Scenario: Version upgrade
- **WHEN** a user updates `spec.module.version` on an existing `ModuleRelease` CR
- **THEN** the controller detects the CR change, re-synthesizes the package with the new version, CUE resolves the new version from the registry, new resources are rendered and applied, and previous resources no longer in the inventory are pruned when `prune: true`
