## 1. Dependency

- [x] 1.1 `go get github.com/open-platform-model/library@v0.3.0`
- [x] 1.2 `go mod tidy`; confirm `go.mod`/`go.sum` resolve cleanly and `cuelang.org/go` stays at `v0.17.0-alpha.1` (no downgrade/conflict)
- [x] 1.3 `task dev:vet` to confirm the operator still compiles with the new dependency in the graph (no code consuming it yet)

## 2. Kernel construction (cmd/main.go)

- [x] 2.1 Construct one `*kernel.Kernel` via `kernel.New(...)` after flag parsing, before controller registration, kept in a variable alive for the manager's lifetime
- [x] 2.2 Configure `kernel.WithRegistry(...)` from the existing `--registry`/`OPM_REGISTRY`-resolved value already used by the legacy loader
- [x] 2.3 Configure `kernel.WithLogger(...)` bridged to the operator's logger (resolve the `*slog.Logger` bridge per design Open Question)
- [x] 2.4 Add a startup core-schema smoke check: resolve the schema once, log the resolved version on success, fail startup (`os.Exit(1)` via `setupLog.Error`) on failure
- [x] 2.5 Extract `verifyCoreSchema(*kernel.Kernel) (string, error)` and add a hermetic `cmd` unit test asserting the smoke-check failure and success paths via `kernel.WithSchemaLoader` fakes

## 3. Reconciler injection (internal/controller)

- [x] 3.1 Add a `Kernel *kernel.Kernel` field to the render-bearing reconciler structs (`ModuleReleaseReconciler`, `ReleaseReconciler`)
- [x] 3.2 Set the field at registration in `cmd/main.go` from the shared Kernel instance; do NOT read it on any render path

## 4. Validation gates

- [x] 4.1 `task dev:manifests dev:generate` — confirm NO diff (no API change this slice)
- [x] 4.2 `task dev:fmt dev:vet`
- [x] 4.3 `task dev:lint`
- [x] 4.4 `task dev:test` — envtest unit/controller tests pass with the dependency wired and the startup smoke check exercised (or its failure path asserted)
- [x] 4.5 Manually confirm startup logs the resolved core schema version against a reachable registry, and fails fast against an unreachable one

## 5. Documentation

- [x] 5.1 Refresh `KERNEL-MIGRATION-NOTES.md` §0 to record library `v0.3.0` landing (no library blocker remains)
- [x] 5.2 Annotate `KERNEL-MIGRATION-NOTES.md` §5.2 that the two `pkg/render` SA1019 sites are `staticcheck` debt resolved by the planned render-core deletion (deferred this slice, out of scope)
