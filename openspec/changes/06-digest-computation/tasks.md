## 1. Digest functions

- [ ] 1.1 Implement `ConfigDigest(values *v1alpha1.RawValues) string` in `internal/status/digests.go`
- [ ] 1.2 Implement `RenderDigest(resources []*core.Resource) (string, error)` with deterministic sorting
- [ ] 1.3 Implement `SourceDigest(artifactDigest string) string` passthrough

## 2. DigestSet and no-op detection

- [ ] 2.1 Define `DigestSet` struct with Source, Config, Render, Inventory fields
- [ ] 2.2 Implement `IsNoOp(current, lastApplied DigestSet) bool`

## 3. Tests

- [ ] 3.1 Write unit tests for `ConfigDigest`: determinism, nil values, content sensitivity
- [ ] 3.2 Write unit tests for `RenderDigest`: order independence, content sensitivity
- [ ] 3.3 Write unit tests for `IsNoOp`: all match, one differs, empty last applied

## 4. Validation

- [ ] 4.1 Run `make fmt vet lint test` and verify all checks pass
