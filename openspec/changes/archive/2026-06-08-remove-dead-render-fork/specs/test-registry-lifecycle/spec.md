## MODIFIED Requirements

### Requirement: End-to-end integration tests
At least one integration test MUST exercise the real renderer
(`render.KernelModuleRenderer`) against the local OCI registry, materializing a
platform from the real catalog, to validate the registry-backed render pipeline:
module acquisition → kernel `SynthesizeRelease` → `Compile` → rendered resources
with inventory entries. The test MUST resolve the catalog from the materialized
platform (the same path the `PlatformReconciler` uses) rather than copying
catalog sources into `test/fixtures/`, so it tracks production composition
automatically. Full apply → `Ready=True` on a live cluster is covered by the
Kind-backed `test/e2e` suite, not this integration-tier test.

#### Scenario: Real-renderer pipeline validated against the registry
- **WHEN** the integration test runs with the local registry available
- **THEN** it constructs `render.KernelModuleRenderer` with a kernel-materialized platform, renders a ModuleRelease, and the rendered resources carry inventory entries and the runtime-identity labels (`managed-by = opm-controller`, non-empty release uuid)

#### Scenario: Catalog resolved from the materialized platform
- **WHEN** the integration test materializes the platform
- **THEN** the catalog is resolved from the registry via the kernel rather than a copy under `test/fixtures/`, so the test automatically tracks production composition
