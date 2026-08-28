## MODIFIED Requirements

### Requirement: Fleet composition and identity shape

The example test module fleet SHALL be `hello`, `hello_web`, `podinfo`, and `redis`, each authored on the core-v2 identity shape: `metadata.name` in snake case equal to the module path's leaf, `metadata.modulePath` the full major-suffixed address (`opmodel.dev/modules/test/<name>@v0`), `metadata.version` a bare SemVer, catalog imports on the versioned `v1beta1` packages of `opmodel.dev/catalogs/opm@v2`. Each module's `moduleinstance.yaml` SHALL pin the module's current published `v`-prefixed version.

The hyphenated `hello-web` name is retired at the source: a core-v2 module cannot carry a hyphen, so the fixture publishes under the renamed path `opmodel.dev/modules/test/hello_web@v0` (a new registry path — the previously published `hello-web@v0` artifacts remain unmodified, and their relocation/deletion is owned by enhancement 0011's `registry-cleanup`).

Each fixture's identity package SHALL declare `Version` as a plain string literal with no default arm and no local `#VersionType`, and its `metadata.version` SHALL reference that literal directly (`version: id.Version`), with no interpolation or other workaround in `metadata`. Every fixture SHALL load through the kernel's module loader (the same loader the controller uses at reconcile) unmodified.

#### Scenario: Fleet renders on the v2 line

- **WHEN** each fleet module's ModuleInstance is applied against a platform subscribed to the v2 catalog
- **THEN** every module renders and reconciles Ready without hand-authored matching labels

#### Scenario: Hyphenated name absent from the fleet

- **WHEN** the fleet is enumerated (publish task, package-renderer tests, bundles)
- **THEN** exactly `hello`, `hello_web`, `podinfo`, `redis` appear and no enumeration carries the hyphenated spelling

#### Scenario: Fixture identity is a literal the kernel loads
- **WHEN** `test/fixtures/modules/*/identity/identity.cue` is read
- **THEN** each declares `Version: "<semver>"` with no `|`, no `*` and no `#VersionType`
- **AND** the matching `module.cue` declares `version: id.Version`
- **AND** the kernel's module loader accepts each fixture tree
