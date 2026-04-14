## Context

The manager Deployment (`config/manager/manager.yaml`) sets `readOnlyRootFilesystem: true` in the container security context. The controller writes to `--cue-cache-dir=/tmp/cue-cache` at startup during `catalog.LoadProvider()`. On a read-only root filesystem, this write fails and the controller cannot start.

## Goals / Non-Goals

**Goals:**
- Controller starts successfully in-cluster with `readOnlyRootFilesystem: true` preserved.
- CUE module cache writes succeed during catalog provider loading.

**Non-Goals:**
- Persistent CUE cache across Pod restarts (emptyDir is ephemeral — acceptable for POC).
- Configurable cache volume size or type.

## Decisions

**Mount an emptyDir at `/tmp`**

Mount a single `emptyDir: {}` volume at `/tmp` rather than a narrow mount at `/tmp/cue-cache`. Rationale:
- The controller also uses `os.CreateTemp` during artifact fetching (`internal/source/fetch.go:65`), which defaults to `/tmp`.
- A single `/tmp` mount covers both CUE cache and temp file needs.
- No size limit — the CUE cache is small (catalog modules are <10 MB total) and temp artifacts are cleaned up after each reconcile.

**Alternative considered**: Mount at `/tmp/cue-cache` only. Rejected because artifact fetch temp files would still fail on the read-only root filesystem.

## Risks / Trade-offs

- [Ephemeral cache] CUE modules re-download on every Pod restart. → Acceptable for POC. The catalog is small and startup latency is negligible.
- [No size limit] emptyDir has no sizeLimit set. → The CUE cache and temp files are bounded by artifact size (MaxArtifactSize = 64 MB). Risk of filling node disk is minimal.
