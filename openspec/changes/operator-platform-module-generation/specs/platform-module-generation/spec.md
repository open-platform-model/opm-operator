## Purpose

The operator-generated platform module: how the Platform CR's typed coordinates become a build-local `#Platform` CUE module on the operator's own disk, how the generated module is validated, and what the CR's status reflects about it. The CR stays the API; the module is derived state the operator owns end to end.

## ADDED Requirements

### Requirement: The reconciler generates a platform module from the CR

On reconciling the singleton Platform CR, the operator SHALL generate a platform CUE module on its own filesystem for that CR generation: a `cue.mod/module.cue` under a reserved-unpublished `opmodel.dev/platforms/…` module path, pinning core and every subscribed catalog (catalog pins taken verbatim from `spec.registry[path].version`), and a `platform.cue` embedding `core.#Platform` with one `#registry` entry per subscription, carrying the catalog by import. A disabled subscription (`enable: false`) SHALL be generated with `enable: false` on its entry, not omitted. Regeneration SHALL be deterministic: the same CR generation produces byte-identical module content.

#### Scenario: A two-catalog CR generates a two-entry module

- **WHEN** the Platform CR subscribes `opmodel.dev/catalogs/opm@v4` at `4.0.1` and `opmodel.dev/catalogs/k8s@v1` at `1.0.0-alpha.2`
- **THEN** the generated `cue.mod` pins both catalogs at exactly those versions, and `platform.cue` carries one importing entry per path

#### Scenario: Deterministic regeneration

- **WHEN** the same CR generation is reconciled twice
- **THEN** the generated module content is byte-identical both times

### Requirement: The CR's version is stamped as the expected-version tripwire

Each generated `#registry` entry SHALL stamp the CR's `spec.registry[path].version` as the entry's expected `version`, which unifies with the schema's derived readout from the imported catalog. The stamp is an assertion, never a second selection mechanism: a generated module whose pinned bytes disagree with the stamped version MUST fail the build at a path naming the entry.

#### Scenario: Wrong bytes become a named conflict

- **WHEN** generation is defective such that the `cue.mod` pin and the stamped entry `version` disagree
- **THEN** building the module fails with a conflict at a path naming that registry entry, before anything renders against it

### Requirement: The generated module is validated by building it

After writing the module, the reconciler SHALL build it through the kernel's shape-gated platform loader against the operator's configured registry. The Ready condition SHALL reflect the outcome: Ready=True when the build succeeds; Ready=False, with the error naming the failing dependency or entry, when a pinned build does not exist, an entry's key disagrees with its imported catalog's declared module path, or the build fails otherwise. Status reason vocabulary replaces the materialize-era reasons.

#### Scenario: Clean build sets Ready

- **WHEN** the generated module's pins name published builds and the build succeeds
- **THEN** the Platform CR reports Ready=True and records the reconciled generation

#### Scenario: Nonexistent pin surfaces on the CR

- **WHEN** `spec.registry` names a catalog version that is not published
- **THEN** the Platform CR reports Ready=False with a message naming the catalog path and version

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
