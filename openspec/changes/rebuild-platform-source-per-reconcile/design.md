# Design: rebuild-platform-source-per-reconcile

## Context

See `proposal.md` § Why. The owner's decision comes from task i4 (operator half) of the kernel plan beta1 walkthrough (2026-10-02/03): a fresh ModFileSource per Platform reconcile, and a regression test with a real registry that fails once then succeeds. The injectable field stays for tests. No enhancement backs this change, so it has no `enhancement.yaml`.

Current state, read 2026-10-03 (opm-operator main at `9835474`, library `v1.0.0-beta.1`, cuelang.org/go `v0.17.1`):

- `internal/controller/platform_controller.go` `modFiles()` (around line 414) returns `r.ModFiles` when it is set. Otherwise it calls `platformmodule.NewRegistry(RegistryConfig{Registry: r.Registry, ClientType: "opm-operator", Env: os.Environ()})`, stores the result in `r.ModFiles`, and returns it. `Reconcile` calls it once, just before `platformmodule.Closure(ctx, src, ...)` (around line 221).
- `platformmodule.NewRegistry` (library `opm/helper/platformmodule/closure.go`) returns `modconfig.NewRegistry(...)`. That is `modcache.New(modregistry.NewClientWithResolver(resolver), cacheDir)`, a `*modcache.Cache` (`mod/modconfig/modconfig.go`, `NewRegistry`).
- `modcache.Cache.ModFile` (`mod/modcache/fetch.go`) is `c.modFileCache.Do(mv, ...)` over a `par.ErrCache[module.Version, *modfile.File]`. `Do` runs its function once per key and keeps the `(value, error)` pair for the life of the cache. Inside that function, `fetchModFileData` reads the disk cache first and goes to the registry only on a miss. A failure is therefore cached in memory, ahead of any later disk or registry check.
- Only the controller writes `r.ModFiles` outside tests. `cmd/main.go` does not set it. Tests inject it in `internal/controller/platform_controller_test.go` (`requiringSource`). The registry-backed recovery spec `test/integration/reconcile/platform_recovery_test.go` sets `r.ModFiles = nil` between its dead-registry phase and its live phase, so it never sees the stickiness.
- That recovery spec already contains the cache-dir handling a registry-hitting phase needs. Both the closure and the build satisfy their pulls from `CUE_CACHE_DIR` when they can, so the spec points the process `CUE_CACHE_DIR` at an empty directory created with `os.MkdirTemp` and walks it back to writable before removing it.

## Goals / Non-Goals

**Goals**

- A failure while resolving module files in one Platform reconcile never decides a later reconcile's result.
- A real registry proves it: the same reconciler, with no field touched and no spec edit, goes from `BuildFailed` to `Ready=True`.
- Tests can still inject a module-file source.

**Non-Goals**

- The library half of i4 (`opm/schema` `val.Err()`; that schema cache stays never-retry). It is a separate library change.
- Changing `platformmodule.NewRegistry` or wrapping CUE's cache in the library.
- Requeue intervals, failure classification, or condition reasons.
- The build path. `kernel.New(kernel.WithRegistry(...))` builds through the kernel's own per-call loader (library ADR-007), which constructs a registry per acquisition (`library/opm/internal/loader/registry.go`, per the i4 research), so it never shares a modcache across reconciles. The section 2 spec proves this end to end, because its phase 2 build has to succeed on the same Kernel that phase 1 used.

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

The Platform reconciler builds one generation at a time (root `AGENTS.md`, "Every kernel call shares nothing"), so per-reconcile sources do not run in parallel against one cache directory. If they ever did, `modcache` takes file locks for its disk writes.

### Reconcile phase impact

- **Source (closure derivation):** the only phase that changes. Each reconcile builds its own source, and a fetch error is wrapped and reported exactly as before (`BuildFailed`, "resolving platform dependencies: ...").
- **Generate, Build, Gate, Status, Store:** unchanged.
- **Requeue:** unchanged. The difference is that the requeued attempt now reaches the registry, or the disk cache, again.

### The regression spec: a flaky front over the suite's registry

The spec lives in `test/integration/reconcile`, beside the recovery spec, and is registry-backed. It gates and skips the way `liveBuildKernelOrSkip` does.

- **Helper `flakyFront`** (test file, package `reconcile_test`) is an `httptest.Server` whose handler is either down or forwarding. While down it answers every request `503 Service Unavailable`. While forwarding it reverse-proxies (`httputil.ReverseProxy`) to the upstream registry host that `modconfig.NewResolver(&modconfig.Config{CUERegistry: reg}).ResolveToLocation(catalogPath, version)` resolves for the catalog. It rewrites the request path's repository prefix from the one the front's own mapping produces to the live `Repository`, and counts the requests it forwards. The reconciler's `Registry` is the live mapping with one extra, longer-prefix entry that sends only the catalog's module path to `127.0.0.1:<port>+insecure`. CUE routes by longest prefix, so core and everything else still resolve live, and the front only sees catalog traffic. Bearer-token auth needs no special handling: the upstream's `WWW-Authenticate` realm is an absolute URL, so CUE's client fetches the token directly and the front forwards the `Authorization` header unchanged.
- **Phase 1:** with an empty `CUE_CACHE_DIR` and the front down, `r.Reconcile` returns a requeue, the Platform is `Ready=False`/`BuildFailed` with the catalog coordinate in the message, and the store holds nothing.
- **Phase 2:** the front switches to forwarding. The test does not touch `r.Kernel`, `r.Registry` or `r.ModFiles` and does not edit the Platform. The next `r.Reconcile` returns no requeue, the Platform is `Ready=True`/`Generated` at the same generation, the store holds the package, and the front has forwarded at least one request. That last check shows the module file came back from the registry, not the disk. `CUE_CACHE_DIR` stays on the empty directory through phase 2 so it really is a registry round trip.
- **Companion spec (pins the upstream cause):** a single `platformmodule.NewRegistry` source is used for `Closure` while the front is down. After the front switches to forwarding, the same source still returns the error, and a new source succeeds. This spec passes before and after the fix. It documents why the fix exists, and it will start failing if a future CUE release makes `ModFile` retry, at which point the per-reconcile rebuild can be revisited.

Before the fix, phase 2 fails: the stored source returns phase 1's cached 503. Section 1's spike confirms this.

### The existing recovery spec stops resetting the source

`r.ModFiles = nil` and its comment come out of `platform_recovery_test.go`. Once `modFiles()` no longer writes the field, the line does nothing, and keeping it would suggest that recovery depends on resetting it. The spec's swap of `r.Kernel` and `r.Registry` stays: it models a different event (the operator pointed at another registry), and `WithRegistry` is construction-only.

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
**Rationale**: the spec has to run a full Reconcile to `Ready`, which needs core and a real catalog. An in-memory registry would have to be seeded with that whole closure. The front reuses the registry the sibling specs already resolve, from GHCR under `task dev:test` and from the seeded job-local registry in PR CI, and adds no dependency to `go.mod`.

## Risks / Trade-offs

- **[Spike] The front's routing.** The longest-prefix mapping and the repository-prefix rewrite are unverified, particularly against GHCR's token flow. If the GHCR path cannot be made to work, the spec runs only when the upstream resolves `Insecure` (the seeded local registry in PR CI and `task dev:test:seeded`) and skips with a named reason otherwise. That still runs it on every PR.
- **Slower spec.** Phase 2 with an empty cache downloads the catalog and, for the build, core. That is the same cost the recovery spec already accepts.
- **Process-wide `CUE_CACHE_DIR`.** The spec changes the process environment the way the recovery spec does, and restores it in `DeferCleanup`. Specs in this suite run serially.
