## 1. Delete the unused packages

- [ ] 1.1 Run `grep -rln 'opm-operator/pkg/errors"\|opm-operator/pkg/resourceorder"' --include='*.go' .` and verify the only hit is `pkg/errors/errors_test.go`; verify `grep -rn 'pkg/errors\|pkg/resourceorder' pkg/core/` is empty.
- [ ] 1.2 Delete `pkg/errors/` and `pkg/resourceorder/`; verify `go build ./...` and `go vet ./...` are green and `ls pkg/` lists only `core`.
- [ ] 1.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `chore(pkg): delete the unused errors and resourceorder copies`
