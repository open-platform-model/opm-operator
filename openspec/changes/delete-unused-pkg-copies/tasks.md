## 1. Confirm the packages are unused

- [ ] 1.1 Run `grep -rln 'opm-operator/pkg/errors"\|opm-operator/pkg/resourceorder"' --include='*.go' .` and verify the only hit is `pkg/errors/errors_test.go`; verify `grep -rn 'pkg/errors\|pkg/resourceorder' pkg/core/` is empty.

## 2. Delete

- [ ] 2.1 Delete `pkg/errors/` and `pkg/resourceorder/`; verify `go build ./...` and `go vet ./...` are green and `ls pkg/` lists only `core`.

## 3. Gates

- [ ] 3.1 Run `task dev:fmt dev:vet dev:lint dev:test`; verify all green.
