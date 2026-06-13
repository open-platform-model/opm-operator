# Delta: module-acquisition

## MODIFIED Requirements

### Requirement: Acquire a module from the registry as a library Module

The operator SHALL acquire a `ModuleRelease`'s target module from the OCI registry as a library `*module.Module`, given the module path, version, and registry mapping. Acquisition SHALL delegate to the library kernel's registry module loader — `Kernel.LoadModuleFromRegistry(ctx, modPath, version)` — and decode the returned value via `Kernel.NewModuleFromValue`. Acquisition SHALL NOT synthesize a wrapper CUE package that imports and embeds the target module, and SHALL NOT write the module's source to a temporary directory.

Delegating to the library is load-bearing: the wrapper approach re-embedded core@v0's `#Module` (collapsing its self-referential `metadata.modulePath`/`version`), could not resolve the target's transitive catalog/core dependencies, and failed the loader's root shape gate. The library loader loads the module as the main module — its own `cue.mod/module.cue` resolves transitive deps and its `kind`/`metadata` sit at the package root — so author-set metadata is preserved and dependencies resolve. Registry configuration and the absence of process-environment mutation are the library's responsibility (the kernel's configured registry); acquisition remains safe to call concurrently from reconcilers.

#### Scenario: Published module is acquired

- **WHEN** acquisition is called with the path and version of a module published in the registry
- **THEN** it returns a `*module.Module` whose decoded metadata (name, version) matches the published module

#### Scenario: Core@v0 self-referential metadata is preserved

- **WHEN** acquisition loads a module authored against `opmodel.dev/core@v0` (whose `#Module` derives `modulePath`/`version` from themselves)
- **THEN** the decoded module's `metadata.modulePath` and `metadata.version` equal the author-set values, with no `field not allowed` admission error

#### Scenario: Module with catalog imports is acquired

- **WHEN** acquisition loads a module whose components import catalog resource definitions (for example `opmodel.dev/catalogs/opm/resources`)
- **THEN** those transitive dependencies resolve and acquisition succeeds without the operator declaring them

#### Scenario: Unresolvable module surfaces a load error

- **WHEN** acquisition is called with a path/version not present in the registry
- **THEN** it returns an error identifying the failed module load

#### Scenario: Acquisition delegates to the kernel, not a wrapper package

- **WHEN** the `internal/moduleacquire` package is inspected after this change
- **THEN** `Acquire` calls `Kernel.LoadModuleFromRegistry` and there is no generated wrapper package, no `writeShim`, and no temporary-directory staging

### Requirement: Acquisition is registry-config driven, not process-global

Acquisition SHALL drive registry resolution from the shared kernel's configured registry mapping (set at construction via `kernel.WithRegistry`), not by mutating process environment. The library's registry module loader applies the mapping through the CUE load configuration, never `os.Setenv`, so acquisition is safe to call concurrently from reconcilers.

#### Scenario: Registry resolution does not mutate process environment

- **WHEN** acquisition is invoked through the shared kernel
- **THEN** the kernel's configured registry mapping is used for the fetch and load
- **AND** the process environment (`os.Environ`) is not modified

## REMOVED Requirements

### Requirement: Acquisition carries no catalog pin

**Reason:** This requirement described the deleted acquisition shim ("the shim depends only on the target module… SHALL NOT declare a catalog dependency… catalog transformers SHALL NOT be required to acquire a module"). Acquisition no longer synthesizes a shim — it delegates to `Kernel.LoadModuleFromRegistry`, which loads the module as the main module. The module's own `cue.mod/module.cue` declares its catalog/core dependencies, and catalog resolution **is** required to acquire a module whose components attach catalog resources. The requirement is obsolete and contradicts the modified "Acquire" requirement.
