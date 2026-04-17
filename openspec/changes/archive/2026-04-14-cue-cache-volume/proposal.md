## Why

The manager Deployment specifies `readOnlyRootFilesystem: true` but the controller writes to `--cue-cache-dir=/tmp/cue-cache` at startup during catalog provider loading. On a read-only filesystem, `catalog.LoadProvider()` will fail because CUE module resolution needs a writable cache directory. This blocks any in-cluster deployment.

## What Changes

- Add an `emptyDir` volume to the manager Deployment for the CUE cache directory.
- Mount it at `/tmp` (covers both CUE cache and any other temp file needs).
- Keep `readOnlyRootFilesystem: true` — the emptyDir provides the writable surface without weakening the security posture.

## Capabilities

### New Capabilities

_None — this is a deployment manifest fix, not a new behavioral capability._

### Modified Capabilities

_None — no spec-level requirements change._

## Impact

- **Files**: `config/manager/manager.yaml` (add volume + volumeMount).
- **Security**: No regression — `readOnlyRootFilesystem` stays true; `emptyDir` is ephemeral and scoped.
- **SemVer**: PATCH — fixes a deployment defect, no API or behavior change.
