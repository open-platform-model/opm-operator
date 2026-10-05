## MODIFIED Requirements

### Requirement: Adapt compiled output to operator resources

The renderer SHALL adapt the single-build render's compiled objects — the kernel's own compiled-output type, declared beside the render verb — to the library's Kubernetes object resources (`opm/k8s/object`) and to inventory entries exactly as before, after first refusing a render whose compiled objects share one Kubernetes apply identity (apiVersion, kind, namespace and name). The refusal SHALL use the library's duplicate-identity check and SHALL carry its error unchanged, naming each shared identity once and every component and transformer that produced it; when it fires, the renderer SHALL build no resource, no inventory entry and no digest, so nothing of that render can reach apply (0015:D15: a second registration in one module is refused before apply naming both carrying components, and 0015:D12 gives both the same instance-derived name).

The render SHALL report no message strings. The renderer SHALL compose the result's warnings itself from the render's advisory diagnostic rows: the unhandled-trait table, and the resolved-versions rows marked newer. Each composed warning SHALL name the same facts as before — for skew, the OPM-namespace path, the version the module requires and the version the platform carries; for an unhandled trait, the component and the trait.

#### Scenario: Compiled item maps to a resource

- **WHEN** a library `Compiled` with a value and provenance is adapted
- **THEN** the resulting library `object.Resource` carries the same value, instance, component, and transformer
- **AND** an inventory entry can be built from it via its `ToUnstructured` conversion

#### Scenario: One compiled-output type

- **WHEN** the operator's adapter names the type it converts from
- **THEN** it SHALL be the type the kernel package declares, converted by the library's `object.Resources`, and the operator SHALL NOT declare a resource type of its own for it

#### Scenario: Warnings reach the reconciler

- **WHEN** the render reports an unhandled optional trait, or a path whose resolved-versions row is marked newer under the `Warn` policy
- **THEN** the render result carries one operator-composed warning naming that fact, and the render result carries no library-authored message string

#### Scenario: Two registrations in one module are refused before apply

- **WHEN** a render's compiled objects include two `TransformerRegistration` values with the same name, produced by two components
- **THEN** adaptation fails with the library's duplicate-identity error naming that identity and both components with their transformers, and the render result carries no resource and no inventory entry

#### Scenario: Distinct identities adapt as before

- **WHEN** every compiled object has a distinct apiVersion, kind, namespace and name
- **THEN** adaptation succeeds and produces one resource and one inventory entry per object
