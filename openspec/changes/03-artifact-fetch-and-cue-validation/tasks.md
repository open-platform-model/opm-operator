## 1. HTTP fetch with digest verification

- [ ] 1.1 Implement `ArtifactFetcher` struct with `Fetch(ctx, url, digest, destDir) error` in `internal/source/fetch.go`
- [ ] 1.2 Add SHA-256 digest verification after download
- [ ] 1.3 Add size limit enforcement (64 MB default)

## 2. Zip extraction

- [ ] 2.1 Implement `extractZip(zipPath, destDir) error` helper in `internal/source/extract.go`
- [ ] 2.2 Add zip path traversal protection (reject `..` components)
- [ ] 2.3 Write unit tests for zip extraction: valid zip, invalid zip, path traversal, empty archive

## 3. CUE module validation

- [ ] 3.1 Implement `ValidateCUEModule(dir string) error` in `internal/source/validate.go`
- [ ] 3.2 Write unit tests for CUE module validation: valid layout, missing cue.mod

## 4. Integration

- [ ] 4.1 Write integration test for full fetch→extract→validate pipeline using a test fixture zip
- [ ] 4.2 Create test fixture: minimal valid CUE module as a zip file

## 5. Validation

- [ ] 5.1 Run `make fmt vet lint test` and verify all checks pass
