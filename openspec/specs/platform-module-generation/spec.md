# platform-module-generation Specification

## Purpose

The operator-generated platform module: how the Platform CR's typed coordinates become a build-local `#Platform` CUE module on the operator's own disk, how the generated module is validated, and what the CR's status reflects about it. The CR stays the API; the module is derived state the operator owns end to end.

## Requirements

### Requirement: The reconciler generates a platform module from the CR

On reconciling the singleton Platform CR, the operator SHALL generate a platform CUE module on its own filesystem through the library's platform-module generator (`opm/helper/platformmodule`): a `cue.mod/module.cue` under the fixed reserved-unpublished module path `opmodel.dev/platforms/cluster@v0`, pinning core at the library's verified core release (the version carried by the kernel's default schema module identifier, so the platform embeds the release the render build's glue, promotion and skew check were measured against) and every catalog the module's registry names, and a `platform.cue` embedding `core.#Platform` with one `#registry` entry per catalog, carrying the catalog by import. The operator SHALL carry no core-pin constant of its own.

The registry SHALL be resolved from BOTH of the two paths transformers take to a platform (enhancement 0015 D3, D13): one entry per subscription the CR's `spec.registry` authored, with the pin taken verbatim from `spec.registry[path].version`, and one entry per accepted-and-active `TransformerRegistration` claim, enabled and pinned at the version the claim named. A catalog is one `#registry` key, so a catalog named by an authored subscription AND by a claim SHALL resolve to a single entry taken from the subscription: the platform admin's pin and enable decision is the deliberate one, and a disabled subscription SHALL NOT be re-enabled by a provider registering against it.

The dependency list SHALL be the full closure: beyond those roots it SHALL pin every module the pinned modules transitively require, at the maximum version any requirement in the closure names (the roots included), derived from the pinned modules' published module files without running a tidy; the module-file source SHALL be constructed from the operator's configured registry mapping, its client type and its process environment, passed explicitly. A disabled subscription (`enable: false`) SHALL be generated with `enable: false` on its entry, not omitted, and its catalog SHALL still be pinned and imported. Regeneration SHALL be deterministic: the same inputs produce byte-identical module content, and that content SHALL be byte-identical to what the operator's previous in-tree generator produced for the same input and core pin, except for the first line of `platform.cue`, the generator attribution header, which names the library helper rather than the operator (nothing reads it). A Platform with no active claim SHALL generate the module its spec alone produces.

#### Scenario: A two-catalog CR generates a two-entry module

- **WHEN** the Platform CR subscribes `opmodel.dev/catalogs/opm@v4` at `4.0.1` and `opmodel.dev/catalogs/k8s@v1` at `1.0.0-alpha.2`
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

### Requirement: The CR's version is stamped as the expected-version tripwire

Each generated `#registry` entry SHALL stamp the CR's `spec.registry[path].version` as the entry's expected `version`, which unifies with the schema's derived readout from the imported catalog. The stamp is an assertion, never a second selection mechanism: a generated module whose pinned bytes disagree with the stamped version MUST fail the build at a path naming the entry.

#### Scenario: Wrong bytes become a named conflict

- **WHEN** generation is defective such that the `cue.mod` pin and the stamped entry `version` disagree
- **THEN** building the module fails with a conflict at a path naming that registry entry, before anything renders against it

### Requirement: The generated module is validated by building it

After writing the module, the reconciler SHALL build it through the kernel's shape-gated platform loader against the operator's configured registry, and after a successful build SHALL read the built platform's contract inventory and record the package only when the inventory is routable and discriminated (the platform-inventory-gate capability). The Ready condition SHALL reflect the outcome: Ready=True with reason `Generated` when the build succeeds and the gate passes; Ready=False with reason `BuildFailed`, with the error naming the failing dependency, entry or inventory field, when a pinned build does not exist (closure derivation or build), an entry's key disagrees with its imported catalog's declared module path, the build fails otherwise, or the inventory cannot be read; Ready=False with reason `GenerateFailed` when the module could not be written to disk; Ready=False with reason `OverSubscribedContracts` or `ComparablePredicates` when the module built and the gate refused it. The materialize-era reasons (`Materialized`, `MaterializeFailed`) are retired. A failed or refused reconcile SHALL leave the previously recorded module (if any) in place.

#### Scenario: Clean build sets Ready

- **WHEN** the generated module's pins name published builds, the build succeeds and the inventory is routable and discriminated
- **THEN** the Platform CR reports Ready=True and records the reconciled generation

#### Scenario: Nonexistent pin surfaces on the CR

- **WHEN** `spec.registry` names a catalog version that is not published
- **THEN** the Platform CR reports Ready=False with reason `BuildFailed` and a message naming the catalog path and version

#### Scenario: A failed build keeps the last good module

- **WHEN** a Platform generation N built successfully and generation N+1 fails to build
- **THEN** the process-local record still names generation N's module directory, and the CR reports Ready=False for generation N+1

#### Scenario: A built but refused module keeps the last good module

- **WHEN** a Platform generation N built and passed the gate and generation N+1 builds but its inventory is not routable or not discriminated
- **THEN** the process-local record still names generation N's module, and the CR reports Ready=False for generation N+1 with the refusal's reason

### Requirement: The generated module lives in a per-identity directory and is swapped whole

The operator SHALL write each package's module under a manager-configured platform directory (`--platform-dir`, default `/tmp/opm-platform`, on the manager's writable `emptyDir`) in a directory named by the package identity, so two packages built for one CR generation from different active-claim sets never share a directory. The name SHALL be stable, bounded and filesystem-safe, and SHALL remain `gen-<generation>` for a package built with no active claim.

Generation SHALL write into a staging directory and rename it into place, so a module directory is either absent or complete; a partially written module SHALL never be recorded as current. After a successful build the operator SHALL keep the current package's directory and every superseded package's directory a render still holds a lease on, and remove every other entry: superseded packages, staging leftovers and moved-aside copies. A superseded package leased at prune time SHALL be removed by a later reconcile once its lease is released. At manager start the operator SHALL empty the platform directory; the module is regenerated by the initial reconcile of the Platform CR, and every render path gates on the process-local record, so nothing renders before the module exists.

#### Scenario: A crash mid-write leaves no current module

- **WHEN** the operator fails after writing some but not all files of a package's module
- **THEN** no directory for that identity exists and the process-local record is unchanged

#### Scenario: Two identities for one generation get separate directories

- **WHEN** a package is generated for generation N with no active claim and another for generation N with one active claim
- **THEN** the two occupy different directories and neither overwrites the other

#### Scenario: Superseded packages are pruned

- **WHEN** three packages have each been generated and built in turn and no render leases any of them
- **THEN** only the current package's directory remains

#### Scenario: A leased package survives the prune

- **WHEN** a render holds a lease on one package while the next is generated and built
- **THEN** the leased directory remains beside the current one, and the next reconcile after the lease is released removes it

#### Scenario: Restart regenerates from the CR

- **WHEN** the manager restarts with a Platform CR present
- **THEN** the platform directory is emptied at start and the first reconcile of the CR regenerates and rebuilds the module before any render is admitted

### Requirement: The generated module is build-local and never published

The generated module SHALL exist only on the operator's own filesystem for the operator's own consumption. The operator MUST NOT publish it to any registry, write it to the cluster, or serve it to other consumers; the reserved `opmodel.dev/platforms/…` namespace stays reserved-unpublished (0019 D6). The module's location and the CR generation it was built for SHALL be recorded process-locally for the render path to consume.

#### Scenario: Nothing leaves the pod

- **WHEN** a Platform CR is reconciled successfully
- **THEN** no registry push, ConfigMap, Secret or other cluster object carries the generated module content

### Requirement: The Platform CR API is unchanged

`PlatformSpec` and `Subscription` SHALL keep their existing shape and validation (path-keyed registry, required bare-SemVer `version`, optional `enable`, cluster-singleton rule). Generation SHALL consume the spec as stored; no new API field is required for this capability.

#### Scenario: Existing CRs reconcile without edits

- **WHEN** a Platform CR valid under the current CRD is reconciled by an operator with this capability
- **THEN** generation proceeds from the stored spec with no schema migration
