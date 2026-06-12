## REMOVED Requirements

### Requirement: CUE composition module for provider unification

**Reason**: The `catalog/` composition module that imported and unified provider transformers is deleted. There is no composed provider — transformers come from the materialized platform's registry subscriptions.
**Migration**: See `platform-reconciler` — subscriptions on the `Platform` CR's `registry` replace the composition module's imports.

### Requirement: Dockerfile copies composition module

**Reason**: With the `catalog/` composition module deleted, the `COPY catalog/ /catalog/` and the `.dockerignore` `!catalog/**` allow-rule are removed from the image build.
**Migration**: None — the image no longer ships a composition module.

## MODIFIED Requirements

### Requirement: Registry resolution at startup

The controller MUST resolve a CUE registry mapping at startup from the `--registry` flag, falling back to the `OPM_REGISTRY` environment variable and then a built-in default, and MUST configure the library Kernel with it (`kernel.WithRegistry`) so module and catalog resolution reach the OCI registry. Startup verification that the registry is reachable is covered by the `library-kernel-runtime` capability (core-schema resolution fails fast on an unreachable or misconfigured registry).

#### Scenario: Registry from flag

- **WHEN** the controller starts with `--registry=opmodel.dev=ghcr.io/open-platform-model,registry.cue.works`
- **THEN** the Kernel is configured with that mapping for module and catalog resolution

#### Scenario: Default registry

- **WHEN** no `--registry` flag and no `OPM_REGISTRY` value are provided
- **THEN** the controller uses its built-in default registry mapping
