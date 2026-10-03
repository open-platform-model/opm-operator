## Why

One failed module-file fetch wedges Platform generation until the operator pod restarts. `PlatformReconciler.modFiles` builds the closure's module-file source once and stores it in `r.ModFiles` for the life of the process. That source is the `*modcache.Cache` that `platformmodule.NewRegistry` returns (cuelang.org/go v0.17.1). Its `ModFile` runs every lookup through `par.ErrCache`, which caches the value and the error per module version for good, and checks neither the disk nor the registry again. So when a fetch fails once, every later reconcile gets the same error back: a registry blip, a cancelled reconcile context, or a Platform pinning a catalog version before that version is published. `failReconcile` requeues, but each retry is served the cached failure, the Platform stays `BuildFailed`, and new claims and catalog bumps stop taking effect across the fleet. The registry-backed recovery spec never sees this. It sets `r.ModFiles = nil` between its phases so the source is rebuilt for the mapping it swaps from dead to live, and that reset, like a pod restart, also throws the sticky error cache away.

Owner decision, 2026-10-03: build a fresh module-file source for every Platform reconcile, and prove it with a regression test against a real registry that fails once and then succeeds. The injectable `ModFiles` field stays for tests.

## What Changes

- **A source per reconcile.** `modFiles` stops writing to `r.ModFiles`. When nothing was injected, each Platform reconcile builds its own source from `Registry`, the client type and the process environment. This costs little: fetched module files stay on disk under `CUE_CACHE_DIR`, so only the in-memory error cache is new each time. An injected `ModFiles` is still used as given.
- **A regression spec with a real registry.** A registry-backed spec in `test/integration/reconcile` puts a flaky HTTP front in front of the suite's registry for the catalog's module path. Phase 1: the front refuses requests and the reconcile ends `BuildFailed`. Phase 2: the front forwards, the same reconciler runs again with no field touched and no spec edit, and the Platform reaches `Ready=True`/`Generated`. Against the unchanged controller, phase 2 fails with phase 1's cached 503.
- **The reset line goes.** The existing recovery spec no longer resets `r.ModFiles` between phases: nothing stores a source any more, so the next reconcile builds one from the swapped mapping by itself. Its empty-cache setup moves into a shared helper both specs use.

## Impact

- **API types:** none. No CRD, DeepCopy or `dist/install.yaml` regeneration.
- **Internal packages:** `internal/controller/platform_controller.go` (`modFiles` and the `ModFiles` field's doc comment).
- **Controllers:** `PlatformReconciler` only. Reconcile phases are unchanged; the closure step gets its source from the current reconcile.
- **Tests:** `test/integration/reconcile` gains the flaky-front helper, shared setup helpers and one spec, and `platform_recovery_test.go` loses its `r.ModFiles = nil` reset. The spec is registry-backed: it skips when no registry is configured, like its siblings, and runs in PR CI and `task dev:test`, where the catalog resolves from GHCR.
- **Downstream consumers:** none. The cli builds its sources per process. The platform build goes through `cue/load` with no registry passed, so CUE builds one per load (cuelang.org/go `cue/load/config.go`).
- **SemVer:** PATCH (bug fix). In beta it ships as the next `1.0.0-beta.N`.
- **Release:** `fix`.
- **Complexity (Principle VII):** this removes state rather than adding it. The only new code is test code: the flaky front, kept to what the spec needs.
- **Not in this change:** the library half of the same owner decision (the `val.Err()` check in `opm/schema`; the schema `Cache` stays never-retry), a committed spec pinning CUE's sticky-error behaviour itself (the reconcile spec run against the unchanged controller is that evidence), a retry wrapper in the library's `NewRegistry`, and any change to requeue intervals or classification.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-reconciler`: the "Build failures requeue on a bounded interval" requirement now states that each reconcile resolves module files through a source of its own, so a failure from an earlier reconcile is never served again. It gains a scenario for recovering from a transient registry failure on the same reconciler.
