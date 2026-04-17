## ADDED Requirements

### Requirement: CUE composition module for provider unification

The controller MUST ship a CUE composition module at `catalog/` (repo root) that imports provider definitions from multiple OPM catalog modules and unifies their transformers into a single composed provider using CUE's `&` operator.

#### Scenario: Composition module structure

- **WHEN** the `catalog/` directory is inspected
- **THEN** it contains `cue.mod/module.cue` with pinned dependency versions and `config.cue` with provider imports and unification

#### Scenario: Provider unification

- **WHEN** the composition module is evaluated with CUE registry access
- **THEN** the `providers.kubernetes` value contains unified `#transformers` from all imported provider modules (opm, kubernetes, gateway_api, cert_manager, k8up)

### Requirement: Registry resolution at startup

The controller MUST resolve CUE module dependencies from an OCI registry at startup by setting the `CUE_REGISTRY` and `OPM_REGISTRY` environment variables before calling `load.Instances()`.

#### Scenario: Successful registry resolution

- **WHEN** the controller starts with `--registry=opmodel.dev=ghcr.io/open-platform-model,registry.cue.works` and the registry is reachable
- **THEN** all catalog module imports are resolved from the registry and the composed provider is loaded

#### Scenario: Registry unreachable

- **WHEN** the controller starts and the configured registry is unreachable
- **THEN** the controller exits with a fatal error indicating registry resolution failure

#### Scenario: Default registry

- **WHEN** no `--registry` flag is provided
- **THEN** the controller uses `opmodel.dev=ghcr.io/open-platform-model,registry.cue.works` as the default

### Requirement: CUE cache directory configuration

The controller MUST set `CUE_CACHE_DIR` to a writable directory before loading modules. The location MUST be configurable via `--cue-cache-dir` flag.

#### Scenario: Default cache directory

- **WHEN** no `--cue-cache-dir` flag is provided
- **THEN** `CUE_CACHE_DIR` is set to `/tmp/cue-cache`

#### Scenario: Custom cache directory

- **WHEN** `--cue-cache-dir=/var/cache/cue` is provided
- **THEN** `CUE_CACHE_DIR` is set to `/var/cache/cue`

### Requirement: Dockerfile copies composition module

The Dockerfile MUST copy the `catalog/` composition directory into the container image. The `.dockerignore` MUST allow CUE files from the `catalog/` directory.

#### Scenario: Container image contains composition module

- **WHEN** the container image is built
- **THEN** the `/catalog/` directory exists with `cue.mod/module.cue` and `config.cue`
