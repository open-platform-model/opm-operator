## MODIFIED Requirements

### Requirement: End-to-end integration tests
At least one integration test MUST exercise the real renderer
(`render.KernelModuleRenderer`) against the local OCI registry, generating a
platform from the real catalog, to validate the registry-backed render pipeline:
module acquisition from the registry (`moduleacquire.Acquire`) → kernel
`SynthesizeInstance` → kernel `Render` → rendered resources with provenance and
runtime-identity labels. The render result carries no inventory entries; the
reconciler builds them from its one export of the resources. The test MUST
resolve the catalog from the generated platform (the same path the
`PlatformReconciler` uses) rather than copying catalog sources into
`test/fixtures/`, so it tracks production composition automatically. Full
apply → `Ready=True` on a live cluster is covered by the Kind-backed `test/e2e`
suite, not this integration-tier test.

#### Scenario: Real-renderer pipeline validated against the registry
- **WHEN** the integration test runs with the local registry available
- **THEN** it constructs `render.KernelModuleRenderer` with a generated platform (built via `platformmodule` generation and `AcquirePlatformFromDir`, recorded with `SetGenerated`), renders a ModuleInstance, and the rendered resources carry the runtime-identity labels (`managed-by = opm-controller`, non-empty module-instance uuid label)

#### Scenario: Catalog resolved from the generated platform
- **WHEN** the integration test generates the platform through `platformmodule` generation and `AcquirePlatformFromDir` and records it with `SetGenerated`
- **THEN** the catalog is resolved from the registry via the kernel rather than a copy under `test/fixtures/`, so the test automatically tracks production composition
