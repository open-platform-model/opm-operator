## Why

One failed module-file fetch wedges Platform generation until the operator pod restarts. `PlatformReconciler.modFiles` builds the closure's module-file source once and stores it in `r.ModFiles` for the life of the process. That source is the `*modcache.Cache` that `platformmodule.NewRegistry` returns (cuelang.org/go v0.17.1). Its `ModFile` runs every lookup through `par.ErrCache`, which caches the value and the error per module version for good, and checks neither the disk nor the registry again. So when a fetch fails once, every later reconcile gets the same error back: a registry blip, a cancelled reconcile context, or a Platform pinning a catalog version before that version is published. `failReconcile` requeues, but each retry is served the cached failure, the Platform stays `BuildFailed`, and new claims and catalog bumps stop taking effect across the fleet. The registry-backed recovery spec hides this. It sets `r.ModFiles = nil` between its phases, which is what a pod restart does.

Owner decision (kernel plan beta1 walkthrough, task i4, operator half): a fresh ModFileSource per Platform reconcile, and a regression test with a real registry that fails once then succeeds. The injectable `ModFiles` field stays for tests.

## What Changes

- **A source per reconcile.** `modFiles` stops writing to `r.ModFiles`. When nothing was injected, each Platform reconcile builds its own source from `Registry`, the client type and the process environment. This costs little: fetched module files stay on disk under `CUE_CACHE_DIR`, so only the in-memory error cache is new each time. An injected `ModFiles` is still used as given.
- **A regression spec with a real registry.** A registry-backed spec in `test/integration/reconcile` puts a flaky HTTP front in front of the suite's registry. Phase 1: the front refuses requests and the reconcile ends `BuildFailed`. Phase 2: the front forwards, the same reconciler runs again with no field touched and no spec edit, and the Platform reaches `Ready=True`/`Generated`. A companion spec pins down the upstream behaviour this change works around: a source that is reused after the failure keeps serving it.
- **The masking line goes.** The existing recovery spec no longer resets `r.ModFiles` between phases, because nothing writes that field any more.

## Impact

- **API types:** none. No CRD, DeepCopy or `dist/install.yaml` regeneration.
- **Internal packages:** `internal/controller/platform_controller.go` (`modFiles` and the `ModFiles` field's doc comment).
- **Controllers:** `PlatformReconciler` only. Reconcile phases are unchanged; the closure step gets its source from the current reconcile.
- **Tests:** `test/integration/reconcile` gains the flaky-front helper and two specs, and `platform_recovery_test.go` loses its `r.ModFiles = nil` reset. The specs are registry-backed, so they skip when no registry is reachable, like their siblings, and run in seeded PR CI.
- **Downstream consumers:** none. The cli builds its sources per process. Kernel acquisition (`library/opm/internal/loader/registry.go`) already builds a registry per fetch.
- **SemVer:** PATCH (bug fix). In beta it ships as the next `1.0.0-beta.N`.
- **Release:** `fix`.
- **Complexity (Principle VII):** this removes state rather than adding it. The only new code is test code: the flaky front, kept to what the specs need.
- **Not in this change:** the library half of i4 (the `val.Err()` check in `opm/schema`; the schema `Cache` stays never-retry), a retry wrapper in the library's `NewRegistry`, and any change to requeue intervals or classification.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-reconciler`: the "Build failures requeue on a bounded interval" requirement now states that each reconcile resolves module files through a source of its own, so a failure from an earlier reconcile is never served again. It gains a scenario for recovering from a transient registry failure on the same reconciler.
