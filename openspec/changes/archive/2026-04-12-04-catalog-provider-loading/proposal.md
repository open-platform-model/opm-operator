## Why

If providers were loaded from module artifacts, whoever publishes the module would control the transform logic that executes in the cluster. This is a security risk: provider transforms produce Kubernetes resources, and untrusted CUE in the artifact could generate malicious manifests. The controller must own the provider so that only vetted transforms execute, establishing a clear trust boundary between module authors (who declare components) and the platform team (who controls how components become Kubernetes resources).

## What Changes

- Ship the OPM catalog (`opmodel.dev@v1`) as part of the controller container image at a known filesystem path (e.g., `/etc/opm/catalog/v1alpha1/`).
- The catalog includes `providers/kubernetes/` with all transformers, plus transitive deps (`core/`, `schemas/`, `resources/`, `traits/`) and vendored `cue.dev/x/k8s.io`.
- Add a controller flag (`--catalog-path`) defaulting to `/etc/opm/catalog/v1alpha1`.
- Load the provider from the controller-owned catalog at startup using `pkg/loader.LoadProvider`.
- Expose the loaded provider so the CUE rendering bridge (change 05) can receive it as a parameter.
- Update the Dockerfile to copy the catalog into the image.

## Capabilities

### New Capabilities

- `catalog-provider-loading`: Load the OPM provider from a controller-owned catalog on the container filesystem, keeping provider transforms under platform team control.

### Modified Capabilities

## Impact

- New `internal/catalog/` package or similar — loads the catalog and extracts the provider at startup.
- `cmd/main.go` — catalog path flag registration and provider loading during manager setup.
- `Dockerfile` — new `COPY` stage for catalog.
- Depends on: change 01 (CLI packages including `pkg/loader`, `pkg/provider`).
- Consumed by: change 05 (CUE rendering bridge receives the pre-loaded provider).
- External dependency: `opmodel.dev@v1` catalog from `github.com/open-platform-model/catalog/v1alpha1`.
- SemVer: MINOR — new capability.
