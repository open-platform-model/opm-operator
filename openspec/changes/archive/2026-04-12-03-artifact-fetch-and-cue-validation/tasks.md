## 1. Test fixtures

- [x] 1.1 Create `internal/source/testdata/minimal-module/` with `cue.mod/module.cue` and `main.cue` (from experiment 001 fixture)
- [x] 1.2 Create `internal/source/testdata/minimal-module.zip` — valid CUE module as zip (matching Flux `operation: copy` output format)
- [x] 1.3 Create `internal/source/testdata/no-cuemod.zip` — zip without `cue.mod/` directory
- [x] 1.4 Create `internal/source/testdata/traversal.zip` — zip with `../escape.txt` entry
- [x] 1.5 Create `internal/source/testdata/empty.zip` — valid zip with zero entries

## 2. Zip extraction

- [x] 2.1 Implement `extractZip(zipPath, destDir) error` helper in `internal/source/extract.go`
- [x] 2.2 Add zip path traversal protection (reject `..` components)
- [x] 2.3 Add `MaxZipFiles` limit enforcement
- [x] 2.4 Write unit tests for zip extraction in `extract_test.go`:
  - valid zip → assert files at expected paths with correct contents
  - path traversal → assert error, assert no files outside dest dir
  - empty archive → assert no error, dest dir empty
  - corrupt/non-zip data → assert error indicates invalid format
  - file count limit exceeded (>10000 entries) → assert error

## 3. HTTP fetch with digest verification

- [x] 3.1 Implement `ArtifactFetcher` struct with `Fetch(ctx, url, digest, destDir) error` in `internal/source/fetch.go`
- [x] 3.2 Add SHA-256 digest verification after download
- [x] 3.3 Add size limit enforcement (64 MB default)
- [x] 3.4 Write unit tests for fetch in `fetch_test.go` using `httptest.Server`:
  - digest match → assert no error, assert extracted files present
  - digest mismatch → assert error, assert no extracted files
  - non-200 response → assert error with status context
  - size limit exceeded → assert error, assert download aborted
  - Flux path quirk: zip served from `.tar.gz` URL → assert treated as zip

## 4. CUE module validation

- [x] 4.1 Implement `ValidateCUEModule(dir string) error` in `internal/source/validate.go`
- [x] 4.2 Write unit tests for CUE module validation in `validate_test.go`:
  - valid layout (`cue.mod/module.cue` present and non-empty) → no error
  - missing `cue.mod/` → `errors.Is(err, ErrMissingCUEModule)`
  - empty `module.cue` (zero bytes) → error
  - `cue.mod` is a file not a directory → error

## 5. Full pipeline integration test

- [x] 5.1 Write `fetch_integration_test.go`: build zip from fixture → compute digest → serve via `httptest.Server` → call `Fetch` → assert `cue.mod/module.cue` and `main.cue` exist with correct content in dest dir
- [x] 5.2 Assert the recovered module tree is structurally valid (validates the experiment 001 finding end-to-end in Go)

## 6. Validation

- [x] 6.1 Run `make fmt vet lint test` and verify all checks pass
