## Why

`make docker-build` produces a local container image but there's no automated step to load it into a Kind cluster. Kind clusters use a separate containerd runtime that can't see the host's Docker/Podman images. Without `kind load docker-image`, the Deployment pulls `controller:latest` and gets `ErrImagePull`.

## What Changes

- Add a Makefile target (e.g., `make kind-load`) that builds the image and loads it into the Kind cluster.
- Wire it into the local dev workflow so `make deploy` for Kind "just works" with a single command or short sequence.
- Set `imagePullPolicy: IfNotPresent` or `Never` in the Kind-specific deployment path (the default `Always` won't work for locally-loaded images).

## Capabilities

### New Capabilities

_None — build/dev tooling._

### Modified Capabilities

_None._

## Impact

- **Files**: `Makefile` (new target), possibly a kustomize overlay or patch for Kind-specific imagePullPolicy.
- **Dependencies**: Requires `kind` CLI (already a dependency for e2e tests).
- **SemVer**: N/A — tooling only.
