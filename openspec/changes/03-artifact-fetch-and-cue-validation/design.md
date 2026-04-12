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

## Testing Strategy

### Test fixtures

Create a shared test fixture directory at `internal/source/testdata/` with:

- `minimal-module/` — a valid CUE module tree (reuse structure from `experiments/001-cue-oci-validation/fixtures/minimal-module/`): `cue.mod/module.cue` and `main.cue`.
- `minimal-module.zip` — the module tree as a zip archive, matching what Flux actually serves when `layerSelector.operation=copy` is configured. Build this programmatically in a `TestMain` or check in a pre-built fixture.
- `no-cuemod.zip` — a zip containing files but no `cue.mod/` directory.
- `traversal.zip` — a zip with a `../escape.txt` entry for path traversal testing.
- `empty.zip` — a valid zip archive with zero entries.

### Unit tests for zip extraction (`extract_test.go`)

Each test builds or loads a fixture zip and asserts specific extraction behavior:

- **Valid zip**: Extract `minimal-module.zip` → assert `cue.mod/module.cue` and `main.cue` exist at expected paths in dest dir. Assert file contents match the fixture.
- **Path traversal**: Extract `traversal.zip` → assert error returned, assert no files written outside dest dir.
- **Empty archive**: Extract `empty.zip` → assert no error, dest dir is empty.
- **Invalid zip (corrupt data)**: Pass a non-zip file (e.g., plain text) → assert error indicates invalid format.
- **File count limit**: Build a zip with >10000 entries programmatically → assert error enforcing `MaxZipFiles`.

### Unit tests for digest verification (`fetch_test.go`)

Use `net/http/httptest.Server` to serve fixture content:

- **Digest match**: Serve `minimal-module.zip`, compute correct SHA-256, pass it to `Fetch` → assert no error, assert extracted files present.
- **Digest mismatch**: Serve `minimal-module.zip`, pass wrong digest → assert error, assert no extracted files remain.
- **Download failure (non-200)**: Return HTTP 404 from test server → assert error with status context.
- **Size limit exceeded**: Serve content larger than `MaxArtifactSize` → assert error, assert download aborted (not fully read).
- **Flux path quirk**: Serve zip content from a URL path ending in `.tar.gz` → assert the fetcher treats it as zip regardless of path suffix.

### Unit tests for CUE module validation (`validate_test.go`)

- **Valid layout**: Directory with `cue.mod/module.cue` (non-empty) → assert no error.
- **Missing cue.mod**: Directory without `cue.mod/` → assert `errors.Is(err, ErrMissingCUEModule)`.
- **Empty module.cue**: `cue.mod/module.cue` exists but is zero bytes → assert error.
- **cue.mod is a file, not a directory**: Assert error.

### Integration test for full pipeline (`fetch_integration_test.go`)

End-to-end test of the fetch→extract→validate chain using `httptest.Server`:

1. Build a zip from the `minimal-module/` fixture in test setup.
2. Compute its SHA-256 digest.
3. Serve it via `httptest.Server`.
4. Call `Fetch(ctx, serverURL, digest, tmpDir)`.
5. Assert: `tmpDir/cue.mod/module.cue` exists and has correct content.
6. Assert: `tmpDir/main.cue` exists and has correct content.
7. Assert: `cue export` would succeed against the tree (validate the module content is structurally sound, not just file-present).

This test proves the full chain works against a payload shaped exactly like what Flux serves from a native CUE OCI artifact — the core finding from experiment 001.

## Risks / Trade-offs

- **[Risk] Large artifacts** — A malicious or oversized artifact could exhaust memory/disk. Mitigation: set a size limit on the HTTP response body (e.g., 64 MB) and a file count limit during zip extraction.
- **[Risk] Zip path traversal** — Malformed zip entries with `../` paths. Mitigation: validate all zip entry paths are clean (no `..` components) before extraction.
