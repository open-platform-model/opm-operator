## MODIFIED Requirements

### Requirement: Render a ModuleRelease through the kernel

`KernelModuleRenderer` SHALL acquire the module from the registry, synthesize the instance (source-carrying) with the supplied values, and render it through the single-build render with the leased platform record and the record's skew policy. The kernel gate SHALL be held only across acquisition and synthesis; the build SHALL run outside it so renders of different objects overlap.

#### Scenario: Renders resources from a materialized platform

- **WHEN** `RenderModule` is called for a resolvable module while a generated platform is recorded
- **THEN** the result carries the rendered resources and inventory entries, and any warnings the build reported

#### Scenario: Values are applied when supplied
- **WHEN** non-nil `RawValues` are passed
- **THEN** they are compiled to a `cue.Value` and supplied to instance synthesis
- **AND** when no values are supplied the module's `#config` defaults apply

### Requirement: Gate rendering on a materialized platform

`KernelModuleRenderer` SHALL return `ErrPlatformNotReady` before any registry I/O when the store holds no generated-module record, and SHALL hold a lease on the record for the duration of the render otherwise.

#### Scenario: Empty store yields ErrPlatformNotReady

- **WHEN** `RenderModule` is called while the store holds no record
- **THEN** it returns `ErrPlatformNotReady` without acquiring the module

### Requirement: Adapt compiled output to operator resources

The renderer SHALL adapt the single-build render's compiled objects to operator resources and inventory entries exactly as before, and SHALL return the build's warnings on the result for the reconciler to report.

#### Scenario: Compiled item maps to a resource

- **WHEN** the build reports a warning (unhandled optional trait, catalog skew under `Warn`)
- **THEN** the render result carries the warning text

#### Scenario: Warnings reach the reconciler
- **WHEN** the build reports a warning (unhandled optional trait, catalog skew under `Warn`)
- **THEN** the render result carries the warning text
