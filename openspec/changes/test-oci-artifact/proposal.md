## Why

The reconcile loop starts by resolving an OCIRepository's artifact — a real OCI artifact containing a CUE module. Without a published test artifact, there's nothing for the controller to fetch, render, and apply. Need a minimal but realistic CUE module packaged as an OCI artifact that can be served from a registry accessible inside Kind.

## What Changes

- Create a minimal test CUE module under `test/fixtures/` (or similar) that defines a simple Kubernetes resource (e.g., a ConfigMap or Namespace).
- Add a Makefile target or script to push this module to a local OCI registry running inside the Kind cluster (or to a temporary registry sidecar).
- Optionally deploy an in-cluster OCI registry (e.g., `distribution/distribution` as a Pod) so the test artifact is fully self-contained without external network access.

## Capabilities

### New Capabilities

_None — test fixture, not controller behavior._

### Modified Capabilities

_None._

## Impact

- **Files**: `test/fixtures/` (new CUE module), `Makefile` or `hack/` script for publishing.
- **Cluster**: May add an in-cluster registry Pod/Service in a test namespace.
- **Dependencies**: Needs OCI push tooling (`flux push artifact`, `oras push`, or `opm push`).
- **SemVer**: N/A — test infrastructure only.
