## Purpose

Define a `KernelModuleRenderer` that implements the operator's `ModuleRenderer`
interface and renders a `ModuleInstance` entirely through the library kernel. It
leases the generated platform record from the platform store, acquires the
target module, synthesizes the instance, renders it through the kernel's
single-build render, and adapts the compiled output into operator resources.
The renderer is gated on a generated platform and is wired into the reconcilers
in production (see `platform-gated-rendering`).

## Requirements

### Requirement: Render a ModuleRelease through the kernel

`KernelModuleRenderer` SHALL acquire the module from the registry, synthesize the instance (source-carrying) with the supplied values, and render it through the single-build render with the leased platform record and the record's skew policy. Every kernel call SHALL share nothing: acquisition, synthesis and the build each evaluate in a context of their own, so renders of different objects overlap with no gate.

Values SHALL reach synthesis as a stack of values sources, each carrying an origin that identifies where the operator read it from, so a values error names that origin rather than an anonymous filename. No acquisition or synthesis call SHALL pass a per-call registry or load-options argument: the registry mapping is the one the shared Kernel was constructed with.

#### Scenario: Renders resources from a generated platform

- **WHEN** `RenderModule` is called for a resolvable module while a generated platform is recorded
- **THEN** the result carries the rendered resources and inventory entries, and any warnings the render reported

#### Scenario: Values are applied when supplied

- **WHEN** non-nil `RawValues` are passed
- **THEN** they are supplied to instance synthesis as a values source whose origin names the CR field they came from
- **AND** when no values are supplied the module's `#config` defaults apply

#### Scenario: A values error names its origin

- **WHEN** the supplied raw values violate the module's `#config` schema
- **THEN** the render fails with an error naming the origin the operator gave that source, not an anonymous filename

### Requirement: Gate rendering on a generated platform

`KernelModuleRenderer` SHALL return `ErrPlatformNotReady` before any registry I/O when the store holds no generated-module record, and SHALL hold a lease on the record for the duration of the render otherwise.

#### Scenario: Empty store yields ErrPlatformNotReady

- **WHEN** `RenderModule` is called while the store holds no record
- **THEN** it returns `ErrPlatformNotReady` without acquiring the module

### Requirement: Adapt compiled output to operator resources

The renderer SHALL adapt the single-build render's compiled objects — the kernel's own compiled-output type, declared beside the render verb — to operator resources and inventory entries exactly as before, after first refusing a render whose compiled objects share one Kubernetes apply identity (apiVersion, kind, namespace and name). The refusal SHALL use the library's duplicate-identity helper and SHALL carry its error unchanged, naming each shared identity once and every component and transformer that produced it; when it fires, the renderer SHALL build no resource, no inventory entry and no digest, so nothing of that render can reach apply (enhancement 0015 D15: a second registration in one module is refused before apply naming both carrying components, and D12 gives both the same instance-derived name).

The render SHALL report no message strings. The renderer SHALL compose the result's warnings itself from the render's advisory diagnostic rows: the unhandled-trait table, and the resolved-versions rows marked newer. Each composed warning SHALL name the same facts as before — for skew, the OPM-namespace path, the version the module requires and the version the platform carries; for an unhandled trait, the component and the trait.

#### Scenario: Compiled item maps to a resource

- **WHEN** a library `Compiled` with a value and provenance is adapted
- **THEN** the resulting `core.Resource` carries the same value, release, component, and transformer
- **AND** an inventory entry can be built from it via the existing `ToUnstructured` path

#### Scenario: One compiled-output type

- **WHEN** the operator's adapter names the type it converts from
- **THEN** it SHALL be the type the kernel package declares, and the operator SHALL NOT import a separate library package for it

#### Scenario: Warnings reach the reconciler

- **WHEN** the render reports an unhandled optional trait, or a path whose resolved-versions row is marked newer under the `Warn` policy
- **THEN** the render result carries one operator-composed warning naming that fact, and the render result carries no library-authored message string

#### Scenario: Two registrations in one module are refused before apply

- **WHEN** a render's compiled objects include two `TransformerRegistration` values with the same name, produced by two components
- **THEN** adaptation fails with the library's duplicate-identity error naming that identity and both components with their transformers, and the render result carries no resource and no inventory entry

#### Scenario: Distinct identities adapt as before

- **WHEN** every compiled object has a distinct apiVersion, kind, namespace and name
- **THEN** adaptation succeeds and produces one resource and one inventory entry per object

### Requirement: The render reports the module's declared version

Both kernel renderers (`KernelModuleRenderer` for a ModuleInstance, `KernelPackageRenderer` for a ModulePackage) SHALL report on `RenderResult.ModuleVersion` the version the rendered instance's source module declares in `#module.metadata.version`, as that module spells it (bare SemVer, for example `0.1.0`), read through the library's public `opm/schema` paths the same way on both paths. A version that cannot be read as a concrete string SHALL be reported as `""` and SHALL NOT fail the render.

#### Scenario: A rendered ModuleInstance reports its module's version

- **WHEN** `RenderModule` renders a module whose metadata declares `version: "0.0.12"`
- **THEN** the result's `ModuleVersion` is `0.0.12`

#### Scenario: A rendered ModulePackage reports the version of the module its instance renders

- **WHEN** `KernelPackageRenderer.Render` renders a package whose instance imports a module declaring `version: "0.0.12"`
- **THEN** the result's `ModuleVersion` is `0.0.12`

#### Scenario: An unreadable version is reported empty

- **WHEN** the instance's `#module.metadata.version` is missing, not concrete, or not a string
- **THEN** `ModuleVersion` is `""` and the render succeeds

### Requirement: The render reports the skew policy it rendered under

Both kernel renderers (`KernelModuleRenderer` for a ModuleInstance, `KernelPackageRenderer` for a ModulePackage) SHALL report on `RenderResult.SkewPolicy` the catalog skew policy of the platform record they leased for the render, spelled as `Platform.spec.skewPolicy` spells it (`Warn` or `Refuse`), beside `RenderResult.PlatformIdentity`. The reconciler builds the recorded render input key from these two, so the key names the platform the render actually used.

#### Scenario: A render under the default policy reports Warn

- **WHEN** a ModuleInstance renders against a platform record whose resolved skew policy is the default
- **THEN** the result's `SkewPolicy` is `Warn` and its `PlatformIdentity` is the record's identity

#### Scenario: A package render under Refuse reports Refuse

- **WHEN** a ModulePackage renders against a platform record whose skew policy is `Refuse`
- **THEN** the result's `SkewPolicy` is `Refuse`

### Requirement: The render reports the instance's contract demand from the kernel

Both kernel renderers (`KernelModuleRenderer` for a ModuleInstance, `KernelPackageRenderer` for a ModulePackage) SHALL report on `RenderResult.RequiredContracts` the contract demand the kernel's render reports on its diagnostics: every `#resources` and `#traits` key of every component of the instance, sorted and deduplicated. The operator SHALL NOT compute the demand itself by reading the instance's components. An empty demand SHALL be reported as an empty, non-nil list. Source: 0013:D24.

The demand is read only from a successful render; this requirement does not change when the reconciler writes it (see `reconcile-loop-assembly`, "The instance's contract demand is recorded on its status").

#### Scenario: A rendered module reports its components' contracts

- **WHEN** `RenderModule` renders a fixture module whose components declare resources and traits
- **THEN** the result's `RequiredContracts` is exactly the sorted, deduplicated set of those contract FQNs

#### Scenario: Both renderers fill the demand the same way

- **WHEN** a ModuleInstance and a ModulePackage render instances of the same module
- **THEN** both results carry the demand the kernel reported for their render, read through the same adapter

#### Scenario: An empty demand is an empty list

- **WHEN** the kernel's render reports no contracts, or reports the list as absent
- **THEN** the result's `RequiredContracts` is an empty, non-nil list

#### Scenario: No operator walk of the components

- **WHEN** the operator's render package is searched for a reader of a component's `#resources` or `#traits`
- **THEN** none exists; the demand comes from the kernel's render diagnostics only
