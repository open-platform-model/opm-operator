## Why

The current sample CR at `config/samples/releases_v1alpha1_modulerelease.yaml` is a scaffold stub with `# TODO(user): Add fields here`. There's no example showing how to wire a ModuleRelease to an OCIRepository with real field values. Without a working sample, users (and the developer testing locally) have no reference for creating a valid CR.

## What Changes

- Replace the stub ModuleRelease sample with a complete, realistic example including:
  - `spec.sourceRef` pointing to an OCIRepository (matching the test fixture from `test-oci-artifact`).
  - `spec.module.path` set to the test module's path.
  - `spec.values` with example configuration.
  - `spec.prune: true`.
- Add a companion OCIRepository sample CR that references the test artifact registry.
- Update the BundleRelease sample stub similarly (minimal but valid fields, noting the controller is not yet implemented).

## Capabilities

### New Capabilities

_None — documentation/samples, not controller behavior._

### Modified Capabilities

_None._

## Impact

- **Files**: `config/samples/releases_v1alpha1_modulerelease.yaml`, `config/samples/releases_v1alpha1_bundlerelease.yaml`, new `config/samples/source_v1_ocirepository.yaml`.
- **User experience**: Developers get a copy-paste starting point for local testing.
- **SemVer**: N/A — samples only.
