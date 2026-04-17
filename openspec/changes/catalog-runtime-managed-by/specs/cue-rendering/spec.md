## MODIFIED Requirements

### Requirement: Runtime labels injection
The render bridge MUST fill the catalog's `#TransformerContext.#runtimeName` field with the value `app.kubernetes.io/managed-by` should take on rendered resources. For the controller, this value is `core.LabelManagedByControllerValue` (`"opm-controller"`). The catalog declares `#runtimeName` as mandatory; CUE evaluation MUST fail if the field is unset, preventing silent fallback to a wrong or empty value. The render bridge MUST NOT use the deprecated `#runtimeLabels` mechanism (removed from the catalog as part of this change).

#### Scenario: Managed-by label present on resources
- **WHEN** rendering completes successfully through `internal/render` or `pkg/render`
- **THEN** every resource in the result carries `app.kubernetes.io/managed-by: opm-controller` in its labels

#### Scenario: Render fails fast when runtime identity not injected
- **WHEN** a code path constructs `#TransformerContext` (directly or through CUE evaluation) without filling `#runtimeName`
- **THEN** CUE evaluation returns an error mentioning the missing required field
- **AND** no resources are produced

#### Scenario: Runtime identity is end-to-end consistent with Go constants
- **GIVEN** the controller's render pipeline executed against a minimal valid `#ModuleRelease`
- **WHEN** the rendered resources are inspected
- **THEN** the value of `metadata.labels["app.kubernetes.io/managed-by"]` exactly equals `core.LabelManagedByControllerValue`
- **AND** the value of `metadata.labels["module-release.opmodel.dev/uuid"]` is non-empty (sanity check that ownership labels continue to flow through the catalog merge)
