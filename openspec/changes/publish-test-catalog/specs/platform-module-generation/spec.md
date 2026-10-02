## MODIFIED Requirements

### Requirement: The reconciler generates a platform module from the CR

On reconciling the singleton Platform CR, the operator SHALL generate a platform CUE module on its own filesystem through the library's platform-module generator (`opm/helper/platformmodule`): a `cue.mod/module.cue` under the fixed reserved-unpublished module path `opmodel.dev/platforms/cluster@v0`, pinning core at the library's verified core release (the version carried by the kernel's default schema module identifier, so the platform embeds the release the render build's glue, promotion and skew check were measured against) and every catalog the module's registry names, and a `platform.cue` embedding `core.#Platform` with one `#registry` entry per catalog, carrying the catalog by import. The operator SHALL carry no core-pin constant of its own.

The registry SHALL be resolved from BOTH of the two paths transformers take to a platform (enhancement 0015 D3, D13): one entry per subscription the CR's `spec.registry` authored, with the pin taken verbatim from `spec.registry[path].version`, and one entry per accepted-and-active `TransformerRegistration` claim, enabled and pinned at the version the claim named. A catalog is one `#registry` key, so a catalog named by an authored subscription AND by a claim SHALL resolve to a single entry taken from the subscription: the platform admin's pin and enable decision is the deliberate one, and a disabled subscription SHALL NOT be re-enabled by a provider registering against it.

The dependency list SHALL be the full closure: beyond those roots it SHALL pin every module the pinned modules transitively require, at the maximum version any requirement in the closure names (the roots included), derived from the pinned modules' published module files without running a tidy; the module-file source SHALL be constructed from the operator's configured registry mapping, its client type and its process environment, passed explicitly. A disabled subscription (`enable: false`) SHALL be generated with `enable: false` on its entry, not omitted, and its catalog SHALL still be pinned and imported. Regeneration SHALL be deterministic: the same inputs produce byte-identical module content, and that content SHALL be byte-identical to what the operator's previous in-tree generator produced for the same input and core pin, except for the first line of `platform.cue`, the generator attribution header, which names the library helper rather than the operator (nothing reads it). A Platform with no active claim SHALL generate the module its spec alone produces.

#### Scenario: A two-catalog CR generates a two-entry module

- **WHEN** the Platform CR subscribes `opmodel.dev/catalogs/opm@v4` at `4.0.1` and `testing.opmodel.dev/catalogs/operator/provider@v0` at `0.1.0`
- **THEN** the generated `cue.mod` pins both catalogs at exactly those versions, and `platform.cue` carries one importing entry per path

#### Scenario: An active claim's catalog is pinned and imported

- **WHEN** a claim for `opmodel.dev/catalogs/k8up@v1` at `1.2.0` is accepted and active and the CR subscribes to neither
- **THEN** the generated `cue.mod` pins that catalog at `1.2.0` and `platform.cue` carries an enabled importing entry for it

#### Scenario: An authored subscription wins over a claim naming its catalog

- **WHEN** the CR subscribes `opmodel.dev/catalogs/k8up@v1` at `1.0.0` with `enable: false` and an active claim names the same catalog at `1.2.0`
- **THEN** the generated module carries one entry for that catalog, pinned at `1.0.0` with `enable: false`

#### Scenario: A transitive dependency is pinned in the closure

- **WHEN** the subscribed `opmodel.dev/catalogs/opm@v4` build requires `cue.dev/x/k8s.io@v0` at `v0.10.0` and the CR names no such path
- **THEN** the generated `cue.mod` pins `cue.dev/x/k8s.io@v0` at `v0.10.0`, so the render module's promoted list covers it and the platform wins that path

#### Scenario: A disabled subscription is generated disabled

- **WHEN** a subscription carries `enable: false`
- **THEN** the generated `cue.mod` still pins its catalog, `platform.cue` still imports it, and the entry is emitted with `enable: false`

#### Scenario: Deterministic regeneration

- **WHEN** the same CR generation and active-claim set are reconciled twice
- **THEN** the module content the operator holds is byte-identical both times

#### Scenario: A claimless platform generates what its spec alone implies

- **WHEN** no claim is accepted and active
- **THEN** the generated module is a function of the `Platform` spec alone

#### Scenario: Core pin follows the library

- **WHEN** the operator is built against a library release whose default schema module names core `v2.0.0-alpha.7`
- **THEN** the generated `cue.mod` pins `opmodel.dev/core@v2` at `v2.0.0-alpha.7` with no operator-side constant involved, and a later library bump changes the pin without an operator code change
