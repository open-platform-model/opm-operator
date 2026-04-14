## Why

The controller watches `OCIRepository` objects from Flux source-controller. Without Flux source-controller installed in the cluster, the OCIRepository CRD doesn't exist and there's no component to reconcile OCI sources into artifacts. This is a hard prerequisite for any local Kind testing.

## What Changes

- Add a Makefile target (e.g., `make install-flux`) that installs Flux source-controller into the current cluster context.
- Use minimal Flux installation — only `source-controller` component, not the full GitOps toolkit.
- Document the dependency in a local development guide or inline Makefile comments.

## Capabilities

### New Capabilities

_None — this is build/dev tooling, not controller behavior._

### Modified Capabilities

_None._

## Impact

- **Files**: `Makefile` (new target), possibly `hack/` helper script.
- **Dependencies**: Requires `flux` CLI or raw manifests for source-controller.
- **Cluster**: Installs Flux source-controller CRDs + Deployment into `flux-system` namespace.
- **SemVer**: N/A — tooling only, no release artifact change.
