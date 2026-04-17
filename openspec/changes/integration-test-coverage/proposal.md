## Why

The reconcile loop has good unit test coverage of individual packages and good integration coverage of error paths, but lacks integration-level tests for several critical happy-path and recovery scenarios. Config changes, stale pruning during normal reconciliation, state recovery transitions, and multi-reconcile status tracking are all untested at the reconcile level. These gaps mean regressions in the most common user-facing flows would go undetected by `make test`.

## What Changes

- Add integration test stubs (with TODO comments) for ~12 missing reconcile-level scenarios across 4 new test files in `test/integration/reconcile/`.
- Add e2e test stubs for ~4 missing scenarios across 2 new test files in `test/e2e/`.
- No production code changes. No spec-level requirement changes. Test-only.

## Capabilities

### New Capabilities

None. This change adds test coverage for existing capabilities.

### Modified Capabilities

None. All capabilities retain their current requirements. This change validates existing behavior, it does not change it.

## Impact

- `test/integration/reconcile/`: 4 new test files with Ginkgo stubs.
- `test/e2e/`: 2 new test files with Ginkgo stubs.
- No changes to `internal/`, `api/`, `cmd/`, or `config/`.
- PATCH-level change (test infrastructure only).
