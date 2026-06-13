## ADDED Requirements

### Requirement: Registry resolution at startup

The controller MUST resolve a CUE registry mapping at startup from the `--registry` flag, falling back to the `OPM_REGISTRY` environment variable and then a built-in default, and MUST configure the library Kernel with it (`kernel.WithRegistry`) so module and catalog resolution reach the OCI registry. Startup verification that the registry is reachable is covered by the `library-kernel-runtime` capability (core-schema resolution fails fast on an unreachable or misconfigured registry).

#### Scenario: Registry from flag

- **WHEN** the controller starts with `--registry=opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`
- **THEN** the Kernel is configured with that mapping for module and catalog resolution

#### Scenario: Default registry

- **WHEN** no `--registry` flag and no `OPM_REGISTRY` value are provided
- **THEN** the controller uses its built-in default registry mapping

### Requirement: CUE cache directory configuration

The controller MUST set `CUE_CACHE_DIR` to a writable directory before loading modules. The location MUST be configurable via `--cue-cache-dir` flag.

#### Scenario: Default cache directory

- **WHEN** no `--cue-cache-dir` flag is provided
- **THEN** `CUE_CACHE_DIR` is set to `/tmp/cue-cache`

#### Scenario: Custom cache directory

- **WHEN** `--cue-cache-dir=/var/cache/cue` is provided
- **THEN** `CUE_CACHE_DIR` is set to `/var/cache/cue`
