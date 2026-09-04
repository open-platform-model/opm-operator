## MODIFIED Requirements

### Requirement: The reconciler generates a platform module from the CR

On reconciling the singleton Platform CR, the operator SHALL generate a platform CUE module on its own filesystem for that CR generation through the library's platform-module generator (`opm/helper/platformmodule`): a `cue.mod/module.cue` under the fixed reserved-unpublished module path `opmodel.dev/platforms/cluster@v0`, pinning core at the library's verified core release (the version carried by the kernel's default schema module identifier, so the platform embeds the release the render build's glue, promotion and skew check were measured against) and every subscribed catalog (catalog pins taken verbatim from `spec.registry[path].version`), and a `platform.cue` embedding `core.#Platform` with one `#registry` entry per subscription, carrying the catalog by import. The operator SHALL carry no core-pin constant of its own. The dependency list SHALL be the full closure: beyond those roots it SHALL pin every module the pinned modules transitively require, at the maximum version any requirement in the closure names (the roots included), derived from the pinned modules' published module files without running a tidy; the module-file source SHALL be constructed from the operator's configured registry mapping, its client type and its process environment, passed explicitly. A disabled subscription (`enable: false`) SHALL be generated with `enable: false` on its entry, not omitted, and its catalog SHALL still be pinned and imported. Regeneration SHALL be deterministic: the same CR generation produces byte-identical module content, and that content SHALL be byte-identical to what the operator's previous in-tree generator produced for the same input and core pin, except for the first line of `platform.cue`, the generator attribution header, which names the library helper rather than the operator (nothing reads it).

#### Scenario: A two-catalog CR generates a two-entry module

- **WHEN** the Platform CR subscribes `opmodel.dev/catalogs/opm@v4` at `4.0.1` and `opmodel.dev/catalogs/k8s@v1` at `1.0.0-alpha.2`
- **THEN** the generated `cue.mod` pins both catalogs at exactly those versions, and `platform.cue` carries one importing entry per path

#### Scenario: A transitive dependency is pinned in the closure

- **WHEN** the subscribed `opmodel.dev/catalogs/opm@v4` build requires `cue.dev/x/k8s.io@v0` at `v0.10.0` and the CR names no such path
- **THEN** the generated `cue.mod` pins `cue.dev/x/k8s.io@v0` at `v0.10.0`, so the render module's promoted list covers it and the platform wins that path

#### Scenario: A disabled subscription is generated disabled

- **WHEN** a subscription carries `enable: false`
- **THEN** the generated `cue.mod` still pins its catalog, `platform.cue` still imports it, and the entry is emitted with `enable: false`

#### Scenario: Deterministic regeneration

- **WHEN** the same CR generation is reconciled twice
- **THEN** the generated module content is byte-identical both times

#### Scenario: Core pin follows the library

- **WHEN** the operator is built against a library release whose default schema module names core `v2.0.0-alpha.7`
- **THEN** the generated `cue.mod` pins `opmodel.dev/core@v2` at `v2.0.0-alpha.7` with no operator-side constant involved, and a later library bump changes the pin without an operator code change
