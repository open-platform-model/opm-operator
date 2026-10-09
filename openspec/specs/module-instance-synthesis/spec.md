## Purpose

The `module-release-synthesis` capability defines how the controller resolves a
`ModuleRelease` CR into a rendered `#ModuleRelease` CUE value by synthesizing a
temporary CUE module on each reconcile and resolving the target module via
CUE-native OCI registry resolution. It replaces Flux source-controller based
artifact fetching for `ModuleRelease`.

## Requirements

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
- **WHEN** a user creates a `ModuleRelease` CR with values that conflict with `#config`
- **THEN** the controller acquires the module, the values are refused against the module's `#config` with an error naming their positions in `spec.values`, and `status.conditions` reports `Ready=False` with reason `RenderFailed` and `Stalled=True`

#### Scenario: Version upgrade
- **WHEN** a user updates `spec.module.version` on an existing `ModuleRelease` CR
- **THEN** the controller detects the CR change, re-synthesizes the package with the new version, CUE resolves the new version from the registry, new resources are rendered and applied, and previous resources no longer in the inventory are pruned when `prune: true`

### Requirement: An unset required config value is refused by instance synthesis

A ModuleInstance whose `spec.values` leave a required `#config` value of its module unset SHALL be refused by instance synthesis, whether or not a component reads the value and whether or not `spec.values` is present. The controller SHALL report the kernel's refusal and SHALL NOT run a check of its own for this state. The message SHALL be `synthesizing release: Kernel.SynthesizeInstance: instance "<name>": not fully concrete: ` followed by one finding for every unset required value, `values.<field>: incomplete value <type> (<file>:<line>:<column>)` with the position of its `#config` declaration, joined by `; `. A nested field SHALL be named by its full path (`values.db.host`). A value a component reads SHALL be named like a value no component reads, and the message SHALL NOT name the place in a component that reads it. The controller SHALL word the findings from the kernel's typed error tree; the kernel's own text names the first finding and counts the rest. The ModuleInstance SHALL report `Ready=False` and `Stalled=True` with reason `RenderFailed`.

#### Scenario: Unset required value that no component reads

- **WHEN** a module declares `#config: note: string` with no default, no component reads `note`, and a ModuleInstance of it sets no `note` in `spec.values`
- **THEN** the render fails with a message that contains `not fully concrete: values.note: incomplete value string (<file>:<line>:<column>)`, and the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: No spec.values at all

- **WHEN** the same module is instantiated by a ModuleInstance without `spec.values`
- **THEN** the render fails with the same message and the same reason

#### Scenario: Two unset required values

- **WHEN** the module also declares `#config: other: int` with no default and a ModuleInstance sets neither `note` nor `other`
- **THEN** the message names both `values.note` and `values.other`, each with the position of its declaration, and does not shorten the second to a count

#### Scenario: Three unset values, one read by a component, one nested

- **WHEN** a module declares `#config: {note: string, db: host: string, greeting: string}` with no defaults, one component reads `greeting`, and a ModuleInstance of it sets none of them
- **THEN** the message names `values.note`, `values.db.host` and `values.greeting`, each as `incomplete value string` with its position, and names no path under `components`
- **AND** the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: Setting one value removes its finding only

- **WHEN** the same ModuleInstance sets `note` and leaves the other two unset
- **THEN** the message names `values.db.host` and `values.greeting` and does not name `values.note`

#### Scenario: The value is set

- **WHEN** the ModuleInstance sets every required value in `spec.values`
- **THEN** the instance is synthesized

### Requirement: A values failure reports every finding with its positions

When instance synthesis refuses the values of a ModuleInstance, the message on the `Ready` condition SHALL carry the kernel's error text up to its first finding, followed by every finding the kernel reported, each with the positions the kernel attributed it to, and SHALL NOT replace findings after the first with a count while the message is inside the condition's limit of 32768 characters; past that limit it SHALL keep whole findings from the first and end with `; and <N> more findings`. The event carries the same text within the event note limit (capability `events-emission`). A registry fetch failure during synthesis SHALL keep its own text. A finding caused by a value in `spec.values` SHALL name its position as `spec.values:<line>:<column>`. The wording SHALL NOT change how the failure is classified: a values failure SHALL report reason `RenderFailed` with `Stalled=True`, and a registry fetch failure during synthesis SHALL still report `ResolutionFailed` without `Stalled` and retry on the backoff.

#### Scenario: A value of the wrong type that a component reads

- **WHEN** a module declares `#config: message: string | *"hello"`, a component reads `message`, and a ModuleInstance sets `message: 42`
- **THEN** the message names `#config.message`, the conflicting values and a position `spec.values:<line>:<column>`, and the ModuleInstance reports `Ready=False` and `Stalled=True` with reason `RenderFailed`

#### Scenario: A value of the wrong type that no component reads

- **WHEN** a module declares `#config: note: string`, no component reads `note`, and a ModuleInstance sets `note: 7`
- **THEN** the message names `#config.note`, the conflicting values and a position `spec.values:<line>:<column>`, with reason `RenderFailed`

#### Scenario: Two values of the wrong type

- **WHEN** a ModuleInstance sets two values that each conflict with `#config`
- **THEN** the message names both fields, each with its positions

#### Scenario: A constraint a value does not meet

- **WHEN** a module declares `#config: port: int & >0 | *80` and a ModuleInstance sets `port: -1`
- **THEN** the message names `#config.port`, `invalid value -1 (out of bound >0)` and a position `spec.values:<line>:<column>`, with reason `RenderFailed`

#### Scenario: A field the module does not declare

- **WHEN** a ModuleInstance sets a field in `spec.values` that the module's `#config` does not allow
- **THEN** the message contains `field not allowed (spec.values:<line>:<column>)`, with reason `RenderFailed`

#### Scenario: A registry fetch failure during synthesis keeps its class

- **WHEN** instance synthesis fails with a registry fetch failure
- **THEN** the ModuleInstance reports `Ready=False` with reason `ResolutionFailed`, no `Stalled` condition, and retries on the exponential backoff
