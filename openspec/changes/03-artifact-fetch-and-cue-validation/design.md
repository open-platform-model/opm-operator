## Context

Experiment 001 (`experiments/001-cue-oci-validation/RESULTS.md`) confirmed:
- Native CUE OCI modules use `application/zip` layer format.
- Flux `OCIRepository` with `layerSelector.operation=copy` preserves the zip payload.
- The artifact path ends in `.tar.gz` but the body is actually a zip file.
- After unzipping, the tree contains `cue.mod/module.cue` and module source files.

The controller needs to: download from Flux's artifact URL → verify digest → detect zip → extract → validate CUE layout.

## Goals / Non-Goals

**Goals:**
- Implement `Fetcher` that downloads an artifact by URL and verifies its SHA-256 digest.
- Extract the artifact as a zip file to a temporary directory.
- Validate the extracted directory contains a valid CUE module layout.
- Return the directory path for downstream CUE loading.

**Non-Goals:**
- Supporting tar.gz artifacts (native CUE modules are always zip).
- Caching downloaded artifacts across reconciliations (future optimization).
- Loading or evaluating the CUE module (that's change 5).

## Decisions

### 1. Direct HTTP fetch with digest verification, not Flux ArchiveFetcher

Flux's `http/fetch.ArchiveFetcher` assumes tar.gz format internally. Since the artifact is actually a zip, use Go's `net/http` client directly with SHA-256 digest verification. This avoids fighting Flux's tar assumptions.

**Alternative considered:** Using `ArchiveFetcher` and post-processing. Rejected because `ArchiveFetcher` extracts tarballs, which would fail on zip content.

### 2. Zip extraction via Go stdlib

Use `archive/zip` from Go's standard library. No external dependency needed. The zip is a flat module tree (no nested archive layers based on experiment results).

### 3. Temp directory lifecycle

The fetcher creates a temp directory and returns its path. The caller (reconcile loop, change 11) is responsible for cleanup via `defer os.RemoveAll(dir)`. The fetcher does not manage lifecycle.

## Risks / Trade-offs

- **[Risk] Large artifacts** — A malicious or oversized artifact could exhaust memory/disk. Mitigation: set a size limit on the HTTP response body (e.g., 64 MB) and a file count limit during zip extraction.
- **[Risk] Zip path traversal** — Malformed zip entries with `../` paths. Mitigation: validate all zip entry paths are clean (no `..` components) before extraction.
