## 1. Bump the library

- [x] 1.1 Run `go get github.com/open-platform-model/library@v1.0.0-beta.9` and `go mod tidy`; verify only the library lines of `go.mod` and `go.sum` change and that `go build ./...` and `go vet ./...` pass
- [x] 1.2 Search `*.go` for `Admit`, `catalog.Source` and `IdentityError`; verify no use of a removed name is left and every `IdentityError` mention is the pointer form
- [x] 1.3 Delete the sentence "Admit is never set: it is for the operator install only" from the comments on `judgeDelete` (`internal/apply/prune.go`) and `Guard` (`internal/apply/guard.go`); verify `git diff` shows comment lines only
- [x] 1.4 `task dev:manifests dev:generate dev:fmt dev:vet dev:lint dev:test docs:bundle:check` and `task operator-module:drift` green with no generated file changed and no test edited, then commit `fix(deps): bump library to v1.0.0-beta.9`
