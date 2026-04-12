## MODIFIED Requirements

### Requirement: Load provider from catalog directory

The controller MUST load the OPM provider from a CUE composition module directory that uses `cue/load` with registry resolution (via `CUE_REGISTRY`) and the existing `pkg/loader.LoadProvider` function. The composition module imports and unifies providers from multiple catalog modules.

#### Scenario: Successful provider load

- **WHEN** the composition directory exists, contains a valid CUE module, the registry is reachable, and the `providers` value defines a `"kubernetes"` entry with unified transformers
- **THEN** the loader returns a `*provider.Provider` with metadata and data populated, including transformers from all composed modules

#### Scenario: Catalog directory not found

- **WHEN** the configured catalog path does not exist
- **THEN** the loader returns an error indicating the catalog directory is missing

#### Scenario: Invalid CUE module

- **WHEN** the catalog directory does not contain a valid CUE module (missing `cue.mod/module.cue`)
- **THEN** the loader returns an error indicating an invalid catalog layout

#### Scenario: Provider not found in composition

- **WHEN** the composition's `providers` value does not contain the requested provider name
- **THEN** the loader returns an error listing the available provider names

#### Scenario: CUE evaluation failure

- **WHEN** the composition CUE files have evaluation errors (e.g., registry resolution failure, unresolvable import)
- **THEN** the loader returns an error with CUE diagnostic context

### Requirement: Configurable catalog path

The controller MUST accept a `--catalog-path` flag to configure the composition module directory location. The default value MUST be `/catalog`.

#### Scenario: Default path

- **WHEN** no `--catalog-path` flag is provided
- **THEN** the controller uses `/catalog`

#### Scenario: Custom path

- **WHEN** `--catalog-path=./catalog` is provided
- **THEN** the controller loads the composition module from `./catalog`
