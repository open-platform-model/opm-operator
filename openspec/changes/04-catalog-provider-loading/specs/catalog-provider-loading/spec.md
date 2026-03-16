## ADDED Requirements

### Requirement: Load provider from catalog directory

The controller MUST load the OPM provider from a CUE catalog directory on the container filesystem using `cue/load` and the existing `pkg/loader.LoadProvider` function.

#### Scenario: Successful provider load

- **WHEN** the catalog directory exists, contains a valid CUE module with vendored dependencies, and the `providers` package defines a `#Registry` with a `"kubernetes"` entry
- **THEN** the loader returns a `*provider.Provider` with metadata and data populated

#### Scenario: Catalog directory not found

- **WHEN** the configured catalog path does not exist
- **THEN** the loader returns an error indicating the catalog directory is missing

#### Scenario: Invalid CUE module

- **WHEN** the catalog directory does not contain a valid CUE module (missing `cue.mod/module.cue`)
- **THEN** the loader returns an error indicating an invalid catalog layout

#### Scenario: Provider not found in registry

- **WHEN** the catalog's `#Registry` does not contain the requested provider name
- **THEN** the loader returns an error listing the available provider names

#### Scenario: CUE evaluation failure

- **WHEN** the catalog CUE files have evaluation errors (e.g., missing vendored dependency)
- **THEN** the loader returns an error with CUE diagnostic context

### Requirement: Provider loaded at controller startup

The provider MUST be loaded once during controller startup, before the manager starts serving reconciliation requests. The loaded provider is injected into the reconciler struct.

#### Scenario: Startup with valid catalog

- **WHEN** the controller starts and the `--catalog-path` points to a valid catalog
- **THEN** the provider is loaded and the controller starts normally

#### Scenario: Startup with invalid catalog

- **WHEN** the controller starts and the catalog cannot be loaded
- **THEN** the controller exits with a fatal error before starting the manager

### Requirement: Configurable catalog path

The controller MUST accept a `--catalog-path` flag to configure the catalog directory location. The default value MUST be `/etc/opm/catalog/v1alpha1`.

#### Scenario: Default path

- **WHEN** no `--catalog-path` flag is provided
- **THEN** the controller uses `/etc/opm/catalog/v1alpha1`

#### Scenario: Custom path

- **WHEN** `--catalog-path=/custom/path` is provided
- **THEN** the controller loads the catalog from `/custom/path`

### Requirement: Catalog in container image

The Dockerfile MUST copy the OPM catalog into the container image at the default catalog path. The catalog MUST include vendored `cue.dev/x/k8s.io` dependencies in `cue.mod/pkg/` so no network access is required at runtime.

#### Scenario: Container image build

- **WHEN** the container image is built
- **THEN** the catalog exists at `/etc/opm/catalog/v1alpha1/` with all required files and vendored dependencies
