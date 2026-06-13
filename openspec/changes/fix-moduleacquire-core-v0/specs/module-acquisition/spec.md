# Delta: module-acquisition

## MODIFIED Requirements

### Requirement: Acquire a module from the registry as a library Module

The operator SHALL acquire a `ModuleRelease`'s target module from the OCI registry as a library `*module.Module`, given the module path, version, and registry mapping. Acquisition SHALL use the shared `*kernel.Kernel`: it SHALL write a shim package whose `cue.mod/module.cue` declares a single dependency on `<path>@<version>` and whose `.cue` file imports the target module and **binds it to a regular (non-definition) field** rather than embedding it at the package root. Acquisition SHALL then call `Kernel.LoadModulePackage` (with the registry override), look up that field on the loaded value, and decode it via `Kernel.NewModuleFromValue`. Any temporary directory created SHALL be removed before the call returns, on success and on failure.

Binding to a field (not root embedding) is load-bearing: core@v0's `#Module` declares self-referential metadata (`modulePath: metadata.modulePath`, `version: metadata.version`). Embedding the module at the shim package root re-evaluates `#Module` and collapses the self-reference, so author-set `modulePath`/`version` are rejected as `field not allowed`. Binding to a regular field preserves the module value's type-embedding chain without re-admission. The shim MUST NOT bind the module to a `#`-prefixed (definition) field, which re-closes and reproduces the failure.

#### Scenario: Published module is acquired

- **WHEN** acquisition is called with the path and version of a module published in the registry
- **THEN** it returns a `*module.Module` whose decoded metadata (name, version) matches the published module
- **AND** no temporary directory remains afterward

#### Scenario: Core@v0 self-referential metadata is preserved

- **WHEN** acquisition loads a module authored against `opmodel.dev/core@v0` (whose `#Module` derives `modulePath`/`version` from themselves)
- **THEN** the decoded module's `metadata.modulePath` and `metadata.version` equal the author-set values, with no `field not allowed` admission error

#### Scenario: Unresolvable module surfaces a load error

- **WHEN** acquisition is called with a path/version not present in the registry
- **THEN** it returns an error identifying the failed module load
- **AND** no temporary directory remains afterward

#### Scenario: Shim binds rather than embeds

- **WHEN** the shim `.cue` file is generated for a module path and version
- **THEN** it imports the target module and assigns it to a regular field (e.g. `out: mod`), not an unkeyed root embedding (`mod`), and not a `#`-prefixed field
