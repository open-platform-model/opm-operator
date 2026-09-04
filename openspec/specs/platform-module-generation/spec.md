# platform-module-generation Specification

## Purpose

The operator-generated platform module: how the Platform CR's typed coordinates become a build-local `#Platform` CUE module on the operator's own disk, how the generated module is validated, and what the CR's status reflects about it. The CR stays the API; the module is derived state the operator owns end to end.

## Requirements

### Requirement: The reconciler generates a platform module from the CR

On reconciling the singleton Platform CR, the operator SHALL generate a platform CUE module on its own filesystem for that CR generation: a `cue.mod/module.cue` under the fixed reserved-unpublished module path `opmodel.dev/platforms/cluster@v0`, pinning core (the operator's compiled-in core version) and every subscribed catalog (catalog pins taken verbatim from `spec.registry[path].version`), and a `platform.cue` embedding `core.#Platform` with one `#registry` entry per subscription, carrying the catalog by import. The dependency list SHALL be the full closure: beyond those roots it SHALL pin every module the pinned modules transitively require, at the maximum version any requirement in the closure names (the roots included), derived from the pinned modules' published module files without running a tidy. A disabled subscription (`enable: false`) SHALL be generated with `enable: false` on its entry, not omitted, and its catalog SHALL still be pinned and imported. Regeneration SHALL be deterministic: the same CR generation produces byte-identical module content.

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

### Requirement: The CR's version is stamped as the expected-version tripwire

Each generated `#registry` entry SHALL stamp the CR's `spec.registry[path].version` as the entry's expected `version`, which unifies with the schema's derived readout from the imported catalog. The stamp is an assertion, never a second selection mechanism: a generated module whose pinned bytes disagree with the stamped version MUST fail the build at a path naming the entry.

#### Scenario: Wrong bytes become a named conflict

- **WHEN** generation is defective such that the `cue.mod` pin and the stamped entry `version` disagree
- **THEN** building the module fails with a conflict at a path naming that registry entry, before anything renders against it

### Requirement: The generated module is validated by building it

After writing the module, the reconciler SHALL build it through the kernel's shape-gated platform loader against the operator's configured registry. The Ready condition SHALL reflect the outcome: Ready=True with reason `Generated` when the build succeeds; Ready=False with reason `BuildFailed`, with the error naming the failing dependency or entry, when a pinned build does not exist (closure derivation or build), an entry's key disagrees with its imported catalog's declared module path, or the build fails otherwise; Ready=False with reason `GenerateFailed` when the module could not be written to disk. The materialize-era reasons (`Materialized`, `MaterializeFailed`) are retired. A failed reconcile SHALL leave the previously recorded module (if any) in place.

#### Scenario: Clean build sets Ready

- **WHEN** the generated module's pins name published builds and the build succeeds
- **THEN** the Platform CR reports Ready=True and records the reconciled generation

#### Scenario: Nonexistent pin surfaces on the CR

- **WHEN** `spec.registry` names a catalog version that is not published
- **THEN** the Platform CR reports Ready=False with reason `BuildFailed` and a message naming the catalog path and version

#### Scenario: A failed build keeps the last good module

- **WHEN** a Platform generation N built successfully and generation N+1 fails to build
- **THEN** the process-local record still names generation N's module directory, and the CR reports Ready=False for generation N+1

### Requirement: The generated module lives in a per-generation directory and is swapped whole

The operator SHALL write each generation's module under a manager-configured platform directory (`--platform-dir`, default `/tmp/opm-platform`, on the manager's writable `emptyDir`) as `gen-<generation>/`. Generation SHALL write into a staging directory and rename it into place, so a module directory is either absent or complete; a partially written module SHALL never be recorded as current. After a successful build the operator SHALL keep the current and the immediately superseded generation's directories and remove older ones and any staging leftovers. At manager start the operator SHALL empty the platform directory; the module is regenerated by the initial reconcile of the Platform CR, and every render path gates on the process-local record, so nothing renders before the module exists.

#### Scenario: A crash mid-write leaves no current module

- **WHEN** the operator fails after writing some but not all files of a generation's module
- **THEN** no `gen-<generation>` directory for that generation exists and the process-local record is unchanged

#### Scenario: Superseded generations are pruned

- **WHEN** generations 1, 2 and 3 have each been generated and built in turn
- **THEN** `gen-3` and `gen-2` remain and `gen-1` has been removed

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
