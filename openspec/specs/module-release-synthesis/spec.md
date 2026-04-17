## Purpose

The `module-release-synthesis` capability defines how the controller resolves a
`ModuleRelease` CR into a rendered `#ModuleRelease` CUE value by synthesizing a
temporary CUE module on each reconcile and resolving the target module via
CUE-native OCI registry resolution. It replaces Flux source-controller based
artifact fetching for `ModuleRelease`.

## ADDED Requirements

### Requirement: ModuleRelease CR spec shape

The `ModuleRelease` CR MUST use CUE module import paths for resolution rather
than a Flux source reference.

- `spec.module.path` MUST be a CUE module import path (e.g.
  `opmodel.dev/modules/cert_manager@v0`).
- `spec.module.version` MUST be a pinned CUE module version (e.g. `v0.2.1`).
- `spec.sourceRef` MUST NOT be present.

The controller MUST reject a `ModuleRelease` CR that has an empty
`spec.module.path` or an empty `spec.module.version`.

#### Scenario: Valid spec accepted
- **WHEN** a `ModuleRelease` CR is submitted with a non-empty `spec.module.path` and `spec.module.version`
- **THEN** the controller accepts the CR and proceeds with synthesis

#### Scenario: Missing module path rejected
- **WHEN** a `ModuleRelease` CR has an empty `spec.module.path`
- **THEN** the controller rejects the CR and reports the failure via status conditions

#### Scenario: Missing module version rejected
- **WHEN** a `ModuleRelease` CR has an empty `spec.module.version`
- **THEN** the controller rejects the CR and reports the failure via status conditions

### Requirement: Registry configuration

The controller MUST accept a `--registry` flag for CUE registry configuration.

The controller MUST read the `OPM_REGISTRY` environment variable as a fallback
when the `--registry` flag value is empty.

The controller MUST set `CUE_REGISTRY` and `OPM_REGISTRY` to the resolved
registry value before any CUE module evaluation.

The controller ships a built-in default `--registry` value routing
`opmodel.dev/*` and `testing.opmodel.dev/*` to `ghcr.io/open-platform-model`
with `registry.cue.works` as a fallback mirror. Operators override by passing
an explicit `--registry=<mapping>`, or disable the built-in default by passing
`--registry=""` (which then falls through to `OPM_REGISTRY`).

Precedence (highest first):

1. `--registry` flag value (including the built-in default when the operator
   does not pass the flag).
2. `OPM_REGISTRY` environment variable (reached only when `--registry` is
   explicitly empty).
3. CUE's built-in default resolution (reached only when both `--registry` and
   `OPM_REGISTRY` are empty).

#### Scenario: Flag value wins
- **WHEN** the controller is started with an explicit `--registry=<mapping>`
- **THEN** `CUE_REGISTRY` and `OPM_REGISTRY` are set to that mapping before CUE evaluation

#### Scenario: Env fallback when flag empty
- **WHEN** the controller is started with `--registry=""` and `OPM_REGISTRY` is set
- **THEN** `CUE_REGISTRY` and `OPM_REGISTRY` are set to the `OPM_REGISTRY` value

#### Scenario: Built-in default applied
- **WHEN** the controller is started without passing the `--registry` flag
- **THEN** the built-in default mapping is used, routing `opmodel.dev/*` and `testing.opmodel.dev/*` to `ghcr.io/open-platform-model` with `registry.cue.works` as a fallback

#### Scenario: CUE default resolution
- **WHEN** both `--registry` is explicitly empty and `OPM_REGISTRY` is unset
- **THEN** CUE's built-in default resolution is used

### Requirement: Release synthesis

On reconcile, the controller MUST synthesize a temporary CUE module containing:

- A `cue.mod/module.cue` declaring dependencies on the target module and the catalog.
- A `release.cue` that imports `#ModuleRelease` from the catalog and the target module,
  binds `#module:` to the imported module, and sets `metadata.name` and
  `metadata.namespace` from the CR.

The controller MUST clean up the temporary directory after evaluation completes,
regardless of whether evaluation succeeded or failed.

#### Scenario: Temporary module synthesized
- **WHEN** the controller begins reconciling a valid `ModuleRelease` CR
- **THEN** a temporary CUE module with `cue.mod/module.cue` and `release.cue` is created with the target module and catalog imports bound and `metadata.name`/`metadata.namespace` set from the CR

#### Scenario: Temporary directory cleaned up on success
- **WHEN** CUE evaluation completes successfully
- **THEN** the temporary directory is removed

#### Scenario: Temporary directory cleaned up on failure
- **WHEN** CUE evaluation fails
- **THEN** the temporary directory is still removed

### Requirement: Reconcile behavior

The controller MUST synthesize a `#ModuleRelease` CUE package from the CR fields
on reconcile, rely on CUE's module system to resolve the target module from the
OCI registry, load the synthesized package, fill values, and pass the resulting
`#ModuleRelease` value to the existing render pipeline.

The controller MUST reconcile when the `ModuleRelease` CR is created or updated.
The controller MUST NOT poll the OCI registry for new module versions.

#### Scenario: Create triggers reconcile
- **WHEN** a new `ModuleRelease` CR is created
- **THEN** the controller synthesizes and resolves the module and renders resources

#### Scenario: Update triggers reconcile
- **WHEN** an existing `ModuleRelease` CR is updated (including a `spec.module.version` change)
- **THEN** the controller re-synthesizes, re-resolves, and re-renders

#### Scenario: No registry polling
- **WHEN** no `ModuleRelease` CR change occurs
- **THEN** the controller does not poll the OCI registry for new module versions

### Requirement: Status reporting

The `status.source` field MAY be updated to reflect:

- The CUE module path and version (from `spec.module`).
- Whether module resolution from the registry succeeded.

The `status.conditions` MUST report:

- `Ready=True` when the module is successfully resolved, rendered, and applied.
- `Ready=False` with reason `ResolutionFailed` when CUE cannot resolve the module
  from the registry.
- `Ready=False` with reason `RenderFailed` when CUE evaluation or rendering fails.
- `Stalled=True` when the failure is not transient (e.g. module path does not exist).

#### Scenario: Success reported
- **WHEN** the module resolves, renders, and applies successfully
- **THEN** `status.conditions` reports `Ready=True`

#### Scenario: Resolution failure reported
- **WHEN** CUE cannot resolve the module from the registry
- **THEN** `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True` when the failure is not transient

#### Scenario: Render failure reported
- **WHEN** CUE evaluation or rendering fails
- **THEN** `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True` when user input must change to resolve the failure

### Requirement: BundleRelease does not depend on Flux source types

The `bundlerelease_controller` MUST NOT import
`github.com/fluxcd/source-controller/api/v1`, MUST NOT declare RBAC markers for
`source.toolkit.fluxcd.io/ocirepositories`, and MUST NOT retain a
`sourcev1.OCIRepository{}` import-keeper in its reconcile body.

BundleRelease is not yet implemented. When it is implemented it will resolve its
sources via CUE-native module resolution consistent with `ModuleRelease`, not
via Flux source-controller.

The `internal/source/` package remains unchanged and retains its `sourcev1`
dependency. It is not wired into any controller today and is kept available for
potential future use.

The `BundleRelease.spec.sourceRef` API field remains in
`api/v1alpha1/bundlerelease_types.go`. Removing the field is a separate future
API change.

#### Scenario: No Flux imports in BundleRelease controller
- **WHEN** inspecting `bundlerelease_controller.go`
- **THEN** no import of `github.com/fluxcd/source-controller/api/v1` is present

#### Scenario: No Flux RBAC markers
- **WHEN** inspecting RBAC kubebuilder markers on the BundleRelease reconciler
- **THEN** no marker for `source.toolkit.fluxcd.io/ocirepositories` is present

#### Scenario: No sourcev1 import-keeper
- **WHEN** inspecting the reconcile body
- **THEN** no `sourcev1.OCIRepository{}` import-keeper expression is present

### Requirement: End-to-end release scenarios

The synthesis flow MUST behave predictably across the common user-facing scenarios.

#### Scenario: Happy path
- **WHEN** a user creates a `ModuleRelease` CR with valid `spec.module.path` and `spec.module.version`
- **THEN** the controller synthesizes the `#ModuleRelease` CUE package, CUE resolves the module from the OCI registry, evaluation produces concrete `components`, the render pipeline generates Kubernetes resources, resources are applied via SSA, and `status.conditions` reports `Ready=True`

#### Scenario: Module not found in registry
- **WHEN** a user creates a `ModuleRelease` CR with a `spec.module.path` that does not exist in the registry
- **THEN** the controller synthesizes the package, CUE fails to resolve the module, and `status.conditions` reports `Ready=False` with reason `ResolutionFailed` and `Stalled=True`

#### Scenario: Invalid values
- **WHEN** a user creates a `ModuleRelease` CR with values that do not satisfy `#config`
- **THEN** the controller synthesizes the package and CUE resolves the module, value validation fails in `ParseModuleRelease`, and `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True`

#### Scenario: Version upgrade
- **WHEN** a user updates `spec.module.version` on an existing `ModuleRelease` CR
- **THEN** the controller detects the CR change, re-synthesizes the package with the new version, CUE resolves the new version from the registry, new resources are rendered and applied, and previous resources no longer in the inventory are pruned when `prune: true`
