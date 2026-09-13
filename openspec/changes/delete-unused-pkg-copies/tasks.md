## 1. Delete the unused copies

- [x] 1.1 Run `grep -rln 'opm-operator/pkg/errors"\|opm-operator/pkg/resourceorder"' --include='*.go' .` and verify the only hit is `pkg/errors/errors_test.go`; verify `grep -rn 'pkg/errors\|pkg/resourceorder' pkg/core/` is empty.
- [x] 1.2 Delete `pkg/errors/` and `pkg/resourceorder/`; verify `go build ./...` and `go vet ./...` are green and `ls pkg/` lists only `core`.
- [x] 1.3 Run `task dev:fmt dev:vet dev:lint dev:test`; verify all green.
- [x] 1.4 Commit as `refactor(pkg): delete unused pkg/errors and pkg/resourceorder copies`.
