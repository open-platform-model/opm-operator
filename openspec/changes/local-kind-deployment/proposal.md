## Why

There's no single command or documented workflow to go from a clean checkout to a running controller in a local Kind cluster. The pieces exist (`make setup-test-e2e`, `make docker-build`, `make deploy`) but they're disconnected — no orchestration, no Flux install step, no image loading, no sample CR application. A developer trying this for the first time will hit multiple undocumented gaps.

## What Changes

- Add a top-level Makefile target (e.g., `make local-run` or `make kind-deploy`) that orchestrates the full sequence:
  1. Create Kind cluster (if not exists).
  2. Install Flux source-controller.
  3. Build controller image and load into Kind.
  4. Deploy CRDs, RBAC, and controller.
  5. Deploy in-cluster OCI registry + test artifact.
  6. Apply sample OCIRepository + ModuleRelease CRs.
- This target composes the individual targets from the other changes into an end-to-end workflow.
- Add a corresponding teardown target (`make local-clean` or `make kind-teardown`).

## Capabilities

### New Capabilities

_None — developer tooling orchestration._

### Modified Capabilities

_None._

## Impact

- **Files**: `Makefile` (orchestration targets), possibly `hack/local-deploy.sh`.
- **Dependencies**: Composes targets from `cue-cache-volume`, `flux-source-controller-setup`, `kind-image-loading`, `test-oci-artifact`, `sample-modulerelease-cr`.
- **SemVer**: N/A — tooling only.
