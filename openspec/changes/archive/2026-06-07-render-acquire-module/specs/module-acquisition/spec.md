## ADDED Requirements

### Requirement: Acquire a module from the registry as a library Module

The operator SHALL acquire a `ModuleRelease`'s target module from the OCI registry as a library `*module.Module`, given the module path, version, and registry mapping. Acquisition SHALL use the shared `*kernel.Kernel`: it SHALL write a shim package whose `cue.mod/module.cue` declares a single dependency on `<path>@<version>` and whose `.cue` file imports and embeds that module at the package root, then call `Kernel.LoadModulePackage` (with the registry override) followed by `Kernel.NewModuleFromValue`. Any temporary directory created SHALL be removed before the call returns, on success and on failure.

#### Scenario: Published module is acquired

- **WHEN** acquisition is called with the path and version of a module published in the registry
- **THEN** it returns a `*module.Module` whose decoded metadata (name, version) matches the published module
- **AND** no temporary directory remains afterward

#### Scenario: Unresolvable module surfaces a load error

- **WHEN** acquisition is called with a path/version not present in the registry
- **THEN** it returns an error identifying the failed module load
- **AND** no temporary directory remains afterward

### Requirement: Acquisition carries no catalog pin

The acquisition shim SHALL depend only on the target module. It SHALL NOT declare a dependency on any catalog module and SHALL NOT pin a catalog version. The OPM core schema SHALL resolve through the kernel's schema cache, and catalog transformers SHALL NOT be required to acquire a module.

#### Scenario: Shim declares only the target module dependency

- **WHEN** the shim is generated for a module path and version
- **THEN** its `cue.mod/module.cue` lists exactly one dependency — the target module at the given version
- **AND** no catalog module path or catalog version appears in the generated files

### Requirement: Acquisition is registry-config driven, not process-global

Acquisition SHALL apply the registry mapping via the kernel loader's per-call option (`LoadOptions.Registry`), not by mutating process environment, so it is safe to call concurrently from reconcilers.

#### Scenario: Registry override is per-call

- **WHEN** acquisition is invoked with a registry mapping
- **THEN** that mapping is used for the load without writing to `os.Environ`
