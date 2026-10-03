# Design: rebuild-platform-source-per-reconcile

## Context

See `proposal.md` § Why. Owner decision, 2026-10-03 (operator half): a fresh module-file source per Platform reconcile, and a regression test with a real registry that fails once and then succeeds. The injectable field stays for tests. No enhancement backs this change, so it has no `enhancement.yaml`.

Current state, read 2026-10-03 (opm-operator main at `9835474`, library `v1.0.0-beta.1`, cuelang.org/go `v0.17.1`):

- `internal/controller/platform_controller.go` `modFiles()` (around line 414) returns `r.ModFiles` when it is set. Otherwise it calls `platformmodule.NewRegistry(RegistryConfig{Registry: r.Registry, ClientType: "opm-operator", Env: os.Environ()})`, stores the result in `r.ModFiles`, and returns it. `Reconcile` calls it once, just before `platformmodule.Closure(ctx, src, ...)` (around line 221).
- `platformmodule.NewRegistry` (library `opm/helper/platformmodule/closure.go`) returns `modconfig.NewRegistry(...)`. That is `modcache.New(modregistry.NewClientWithResolver(resolver), cacheDir)`, a `*modcache.Cache` (`mod/modconfig/modconfig.go`, `NewRegistry`).
- `modcache.Cache.ModFile` (`mod/modcache/fetch.go`) is `c.modFileCache.Do(mv, ...)` over a `par.ErrCache[module.Version, *modfile.File]`. `Do` runs its function once per key and keeps the `(value, error)` pair for the life of the cache. Inside that function, `fetchModFileData` reads the disk cache first and goes to the registry only on a miss. A failure is therefore cached in memory, ahead of any later disk or registry check.
- Only the controller writes `r.ModFiles` outside tests. `cmd/main.go` does not set it. Tests inject it in `internal/controller/platform_controller_test.go` (`requiringSource`). The registry-backed recovery spec `test/integration/reconcile/platform_recovery_test.go` sets `r.ModFiles = nil` between its dead-registry phase and its live phase. It needs that because it swaps the mapping from dead to live and a stored source would keep the dead one; the same reset also throws the sticky error cache away, so the spec never sees the stickiness.
- That recovery spec already contains the cache-dir handling a registry-hitting phase needs. Both the closure and the build satisfy their pulls from `CUE_CACHE_DIR` when they can, so the spec points the process `CUE_CACHE_DIR` at an empty directory created with `os.MkdirTemp` and walks it back to writable before removing it. It restores the variable in the middle of the test, before its live phase, so that phase runs against the warm process cache.

## Goals / Non-Goals

**Goals**

- A failure while resolving module files in one Platform reconcile never decides a later reconcile's result.
- A real registry proves it: the same reconciler, with no field touched and no spec edit, goes from `BuildFailed` to `Ready=True`.
- Tests can still inject a module-file source.

**Non-Goals**

- The library half of the same owner decision (`opm/schema` `val.Err()`; that schema cache stays never-retry). It is a separate library change.
- Changing `platformmodule.NewRegistry` or wrapping CUE's cache in the library.
- Requeue intervals, failure classification, or condition reasons.
- The build path. The platform build is `Kernel.AcquirePlatformFromDir`, which loads through `cue/load` with no `Registry` in its config, so CUE calls `modconfig.NewRegistry` for every load (cuelang.org/go v0.17.1 `cue/load/config.go:430`) and no module cache outlives one build. This change does not test that path: in the regression spec's phase 1 the closure fails before the build runs.

## Decisions

### A source per reconcile; an injected source wins

```go
// modFiles returns the module-file source for one reconcile's closure
// derivation. An injected ModFiles is returned as is. Otherwise a new
// source is built on every call from the registry mapping, the client type
// and the process environment (CUE_CACHE_DIR is set at manager start):
// CUE's module cache keeps every lookup error in memory for the life of the
// source, so reusing one across reconciles would serve a transient failure
// to every retry. Fetched module files persist on disk under CUE_CACHE_DIR,
// so a new source costs no extra downloads.
func (r *PlatformReconciler) modFiles() (platformmodule.ModFileSource, error) {
	if r.ModFiles != nil {
		return r.ModFiles, nil
	}
	return platformmodule.NewRegistry(platformmodule.RegistryConfig{
		Registry:   r.Registry,
		ClientType: "opm-operator",
		Env:        os.Environ(),
	})
}
```

The `ModFiles` field's doc comment changes to match: "Nil builds a fresh source per reconcile; a test may inject a fixture graph, which is used across reconciles as given." `Reconcile` already calls `modFiles()` exactly once, so this puts one source in each reconcile.

The Platform is the singleton `cluster`, and controller-runtime never reconciles one key concurrently, so per-reconcile sources do not run in parallel against one cache directory. If they ever did, `modcache` takes file locks for its disk writes.

### Reconcile phase impact

- **Source (closure derivation):** the only phase that changes. Each reconcile builds its own source, and a fetch error is wrapped and reported exactly as before (`BuildFailed`, "resolving platform dependencies: ...").
- **Generate, Build, Gate, Status, Store:** unchanged.
- **Requeue:** unchanged. The difference is that the requeued attempt now reaches the registry, or the disk cache, again.

### The regression spec: a flaky front over the suite's registry

The spec lives in `test/integration/reconcile`, beside the recovery spec, and is registry-backed. It gates through `liveBuildKernelOrSkip`, so it skips when no registry is configured and fails under `OPM_TEST_REGISTRY_FORCE=1`.

- **Helper `flakyFront`** (test file, package `reconcile_test`) is an `httptest.Server` whose handler is either down or forwarding. While down it answers every request `503 Service Unavailable`. While forwarding it reverse-proxies to the upstream registry host that `modconfig.NewResolver(&modconfig.Config{CUERegistry: live}).ResolveToLocation(catalogBasePath, "")` resolves, using `httputil.ReverseProxy` with `Rewrite` and `pr.SetURL(upstream)`, so the outbound `Host` is the upstream's. It counts refused and forwarded requests. Its mapping is the live mapping plus one longer-prefix entry, `<catalogBasePath>=127.0.0.1:<port>/<upstream repository prefix>+insecure`. CUE routes by longest prefix, so only catalog traffic reaches the front, and the entry carries the upstream's repository prefix, so the repository the client asks for is the upstream's byte for byte: no path rewrite, and the bearer-token scope the client fetches from the upstream's absolute realm URL matches. The helper asserts that the fronted and live repositories are equal.
- **One mapping for both seams.** The reconciler's `Kernel` and `Registry` both use the front's mapping, as `cmd/main.go` gives the operator one mapping for both.
- **Phase 1:** with an empty `CUE_CACHE_DIR` and the front down, `r.Reconcile` returns a requeue, the front has refused at least one request, the Platform is `Ready=False`/`BuildFailed` with the catalog's module path in the message, and the store holds nothing.
- **Phase 2:** the front switches to forwarding. The test does not touch `r.Kernel`, `r.Registry` or `r.ModFiles` and does not edit the Platform. The next `r.Reconcile` ends `Ready=True`/`Generated` with no requeue at the same generation, the store holds the package, and the front has forwarded at least one request. That last check shows the module file came back from the registry, not the disk. `CUE_CACHE_DIR` stays on the empty directory through phase 2 so it really is a registry round trip.

The spike ran this spec against the unchanged controller on 2026-10-03, with the GHCR mapping and `opmodel.dev/catalogs/opm@v4`. Phase 1 passed and phase 2 failed with phase 1's cached error: `resolving platform dependencies: resolving dependency opmodel.dev/catalogs/opm@v4.4.4: module opmodel.dev/catalogs/opm@v4.4.4: 503 Service Unavailable: ... body "registry unavailable\n"`. With the fix, the same route through the front reaches `Ready=True`, so the GHCR token flow works through the front and no fallback is needed. PR CI resolves the catalog from GHCR too (its mixed mapping sends `opmodel.dev` to GHCR), so the spec runs there.

### Shared setup helpers

The empty-cache setup moves out of the recovery spec into `useEmptyCUECache`, which returns an explicit restore func and also registers it with `DeferCleanup`. The recovery spec keeps calling restore before its live phase; the regression spec deliberately does not. The Platform create-and-cleanup block moves into `createClusterPlatform`, which both specs use.

### The existing recovery spec stops resetting the source

`r.ModFiles = nil` and the sentence of its comment that explains it come out of `platform_recovery_test.go`. Once `modFiles()` no longer stores a source, the next reconcile builds one from the swapped mapping by itself, so the line does nothing, and keeping it would suggest that recovery depends on resetting it. The spec's swap of `r.Kernel` and `r.Registry` stays: it models a different event (the operator pointed at another registry), and `WithRegistry` is construction-only.

## Research & Decisions

### Where to break the stickiness

**Context**: the error is cached inside CUE's `modcache.Cache`, a third-party type the operator does not control.
**Explored**: (a) drop `r.ModFiles` only when `Closure` fails; (b) a library-side `NewRegistry` wrapper that never serves a cached error; (c) a fresh source per reconcile.
**Decision**: (c), the owner's choice.
**Rationale**: (a) keeps a long-lived cache whose other failure modes (a context cancelled mid-walk and cached as an error for a coordinate that later succeeds) need the same reset anyway. (b) changes a library API for all consumers, while the cli builds per process and kernel acquisition builds per fetch, so neither needs it. (c) deletes state, and costs nothing that matters because module files persist on disk.

### Why the test needs a real registry

**Context**: a fake `ModFileSource` that fails once and then succeeds would pass against the buggy code, because the stickiness lives in `par.ErrCache` inside `modcache.Cache`, not in the operator.
**Explored**: an in-process OCI registry (`ociserver` over `ocimem`, seeded through `modregistry`), and an HTTP front over the suite's existing registry.
**Decision**: the HTTP front over the suite's registry. This is the "registrytest/zot fixture or similar" the owner named.
**Rationale**: the spec has to run a full Reconcile to `Ready`, which needs core and a real catalog. An in-memory registry would have to be seeded with that whole closure. The front reuses the registry the sibling specs already resolve (the catalog comes from GHCR under both `task dev:test` and PR CI) and adds no dependency to `go.mod`.

### No committed spec for CUE's own behaviour

**Context**: a second spec could pin CUE's sticky `par.ErrCache` directly: one reused source keeps failing after the front recovers.
**Decision**: not committed. The owner asked for one regression test. Such a spec would go red when a CUE release changes `ErrCache` semantics, failing operator CI on a routine `cuelang.org/go` bump for a reason unrelated to the operator. The spike (the regression spec against the unchanged controller) is the evidence.

## Risks / Trade-offs

- **GHCR through the front.** The spec depends on GHCR's anonymous token flow working through a local front. The spike shows it does. If GHCR ever changes that, the spec fails loudly under `OPM_TEST_REGISTRY_FORCE=1` rather than skipping.
- **Slower spec.** Phase 2 with an empty cache downloads the catalog and, for the build, core. That is the same cost the recovery spec already accepts.
- **Process-wide `CUE_CACHE_DIR`.** The spec changes the process environment the way the recovery spec does, and restores it in `DeferCleanup`. Specs in this suite run serially.
