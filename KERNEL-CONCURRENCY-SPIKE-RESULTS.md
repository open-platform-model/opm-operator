# Kernel Concurrency Spike — Results & Handoff Dossier

> **Status:** spike COMPLETE, decision reached. The spike lived on library branch
> `spike/concurrent-render-v0170`, which is being **deleted**. This dossier is the
> durable record so a future session can open a fresh branch and implement the real
> change without redoing the investigation.
> **Date:** 2026-05-31. **Sibling doc:** [`KERNEL-MIGRATION-NOTES.md`](./KERNEL-MIGRATION-NOTES.md)
> (operator rewrite plan; §8 = Platform CRD registration decisions).

---

## 0. TL;DR / Decision

**Question:** can the library render many releases concurrently against one shared,
long-lived `*MaterializedPlatform` — race-clean *and* actually parallel — and what
must change in the kernel to allow it?

**Answer: YES on CUE v0.17, NO on v0.16. Adopt the v0.17 concurrent-render model.**
The kernel needs **no redesign** — one localized change in `opm/compile`. But v0.17
adoption is a **cross-repo precondition** (v0.17 is alpha; the published catalogs
don't parse under it yet).

Decision matrix landed on: **race-clean YES + speedup REAL → adopt.**

---

## 1. Why this exists (the gap)

The `opm-operator` rewrite onto the kernel needs to render many `ModuleRelease`s
concurrently against one `*MaterializedPlatform` that is materialized **once** (per
Platform-CR generation) and reused. Every other operator need is already satisfied
(Kernel lifetime, `sync.Once` schema cache, `Materialize` + concurrency-safe `LRU`).
The render path was the one gap.

Two v0.16.1 blockers:
1. **Same-context coupling.** `*MaterializedPlatform.Package` is built with its
   owning Kernel's `*cue.Context`. The compile pipeline takes its context from the
   platform and `FillPath`s release components in. If the release was built by a
   *different* Kernel → cross-context combination.
2. **Single-threaded Kernel.** `kernel/doc.go`: "NOT safe for concurrent use … one
   Kernel per goroutine."

CUE v0.17 reportedly relaxes both (cross-context `Unify`/`FillPath` legal,
`Value.Context()` deprecated; `cue.Value` reads race-safe) — but the verified v0.17
guarantee is *reads only*, "safe ≠ parallel," and it's alpha. So: measure first.

---

## 2. The decided architecture

```
PlatformReconciler: K0.Materialize(platform)  →  ONE shared, read-only *MaterializedPlatform
                                                  (Package in K0's *cue.Context)
each render goroutine: its OWN Kernel Kn  →  Kn.Compile(release[Kn ctx], sharedPlatform[K0 ctx])
                                              builds in Kn's context, cross-READS the shared platform
```

- Per-goroutine Kernels (the library's already-blessed "one Kernel per goroutine").
- One shared, read-only materialized platform — NOT a shared mutable Kernel.
- No mutex, no re-materialize.
- Rejected alternatives: shared Kernel + caller mutex (serializes, loses the ~2.5×);
  per-goroutine Kernels each re-`Materialize` (defeats materialize-once). Both remain
  the **v0.16 fallback** if v0.17 adoption is deferred.

---

## 3. Spike results (exact)

### 3a. Keystone — raw CUE, no library/registry/fixtures

Isolated module `.spike/crosscontext/` (own go.mod, swap the CUE pin between runs).
32 goroutines × 200 iters of concurrent cross-context `FillPath`, under `-race`:

| | result |
|---|---|
| **v0.16.1** | 💥 `panic: values are not from the same runtime` (at `FillPath`) |
| **v0.17.0-alpha.1** | ✅ race-clean |

Scaling benchmark (AMD Ryzen 7 8700G, 16 cores), serial vs `RunParallel`:

| `-cpu` | FillSerial ns/op | FillConcurrent ns/op | speedup |
|---|---|---|---|
| 1 | 11943 | 11858 | 1.0× (== serial at 1 core ✓) |
| 2 | 10445 | 6915 | **1.7×** |
| 4 | 10420 | 4646 | **2.6×** |
| 8 | 10763 | 4843 | ~2.5× (plateaus ~4 cores) |

→ v0.17 gives **genuine parallelism (~2.5×), not just correctness.** Plateau ~4 cores
is allocator-bound (145 allocs/op), not a concurrency ceiling.

### 3b. Kernel-level — cross-kernel `Compile` with real fixtures

Build-tagged `opm/kernel/spike_crosskernel_test.go` (now removed). `K0` materializes
the real `modules/opm_platform`; per-goroutine `Kn` synth+`Compile` the `web_app`
fixture against the shared `mp`:

- **v0.16.1 control:** FAILS — single-threaded returns an error, concurrent panics
  under `-race` — at **`compile.Match` → `unifyIntersection` (`opm/compile/match.go:263`)**,
  the always-unify step combining component primitives (Kn) with transformer maps (K0).
  → the cross-context boundary is at **Match**, not only Execute.
- **v0.17.0-alpha.1:** the library **compiles cleanly** (no removed-API breakage), BUT
  the run dies *before Compile* — the published catalog **`opmodel.dev/catalogs/opm@v0.4.0`
  fails to parse** under v0.17: `import failed: ... missing ',' in argument list`.

→ The cross-context *mechanism* is proven by 3a; the kernel-level v0.17 confirmation is
**blocked at the fixture layer**, not the kernel.

---

## 4. The two findings that shape the real change

1. **Cross-context boundary = `compile.Match` (`unifyIntersection`, `match.go:263`) AND
   Execute's `FillPath`.** The compile pipeline sources its context from the platform
   (`opm/compile/module.go:112`: `cueCtx := r.platform.Package.Context()`) — harmless
   today (single-Kernel callers → same context) but wrong for concurrency: it funnels
   every render through the platform's one context (serializing) and uses the
   v0.17-deprecated `Value.Context()`.

2. **v0.17 adoption is cross-repo, not library-only.** Library code compiles on v0.17,
   but the published catalogs (and likely modules) must be **re-vetted and re-published**
   to parse under v0.17's stricter parser before v0.17 can be pinned.

---

## 5. The real change to implement (the recontract)

A separate, spec-bearing library change — **narrow, not a redesign:**

- **`opm/compile`:** build/unify in the **caller-Kernel's** context and cross-*read*
  the shared platform, at **both** cross-context points (`compile.Match`/`unifyIntersection`
  and Execute's `FillPath`). Replace `r.platform.Package.Context()` (`module.go:112`)
  with an explicitly-threaded `*cue.Context`. **This one change is simultaneously the
  v0.17 deprecation fix and the parallelism enabler.**
- **Pin CUE → v0.17** (only once stable, or under explicit alpha-risk acceptance).
- **Audit `Value.Context()` deprecation** across `compile`/`materialize`/`schema`.
- **Rewrite specs:** `kernel-runtime` §"Goroutine Safety Contract" and
  `platform-materialization` §"MaterializedPlatform Output Shape" → "a
  `*MaterializedPlatform` is immutable and safe to share across goroutines and Kernels;
  `Compile` may be called concurrently from per-goroutine Kernels against one shared
  platform." Rewrite `opm/kernel/doc.go`'s "one Kernel per goroutine" section.
- **Permanent `-race` regression test** against a **v0.17-compatible in-memory catalog**
  (`opm/internal/registrytest` — `BuildCatalog`/`BuildPlatform`), NOT the published
  `@v0.4.0` (which won't parse on v0.17).

### Preconditions / blockers (do NOT pin v0.17 until both hold)
1. A **stable** CUE v0.17 release (or explicit acceptance of alpha risk).
2. **Re-published catalogs/modules** that parse under v0.17.

### Operator decoupling (unchanged)
The operator ships on **v0.16 + a render-path mutex** regardless, and drops the mutex
**only when this recontract lands.** Nothing here is on the operator's critical path.

---

## 6. Reproduction — the keystone test (verbatim, recreatable)

`.spike/crosscontext/go.mod`:
```
module spike/crosscontext
go 1.26
require cuelang.org/go v0.16.1   // swap to v0.17.0-alpha.1 for the candidate run
```

`.spike/crosscontext/crosscontext_test.go` (the decisive parts):
```go
package crosscontext

import (
	"fmt"
	"sync"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// v0.16: panics "values are not from the same runtime"; v0.17: race-clean.
func TestConcurrentCrossContextFill(t *testing.T) {
	ctx0 := cuecontext.New()
	shared := ctx0.CompileString(`{ output: { kind: "Demo", spec: { name: string } } }`)
	if err := shared.Err(); err != nil { t.Fatalf("compile shared: %v", err) }
	sharedOut := shared.LookupPath(cue.ParsePath("output"))

	const goroutines, iters = 32, 200
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctxN := cuecontext.New() // one context per goroutine = one Kernel per goroutine
			want := fmt.Sprintf("c-%d", id)
			comp := ctxN.CompileString(fmt.Sprintf(`{ spec: name: %q }`, want))
			compSpec := comp.LookupPath(cue.ParsePath("spec"))
			for j := 0; j < iters; j++ {
				out := sharedOut.FillPath(cue.ParsePath("spec"), compSpec) // cross-context
				if err := out.Validate(cue.Concrete(true)); err != nil { errs <- err; return }
				got, _ := out.LookupPath(cue.ParsePath("spec.name")).String()
				if got != want { errs <- fmt.Errorf("g%d got=%q want=%q", id, got, want); return }
			}
		}(i)
	}
	wg.Wait(); close(errs)
	for err := range errs { if err != nil { t.Fatal(err) } }
}

// Benchmarks: BenchmarkFillSerial (one ctx, serial loop) vs BenchmarkFillConcurrent
// (b.RunParallel, per-goroutine ctx, cross-context Fill vs ONE shared value).
// Run: go test -bench=Fill -benchmem -run='^$' -cpu=1,2,4,8
```

Run recipe:
```bash
# control
cd .spike/crosscontext && go mod tidy && go test -race -v ./...
# candidate
go get cuelang.org/go@v0.17.0-alpha.1 && go test -race -v ./...
go test -bench=Fill -benchmem -run='^$' -cpu=1,2,4,8
```

### Kernel-level cross-kernel test (the shape to recreate for the regression test)
Fork `opm/kernel/flow_synth_integration_test.go`. Build-tag it. Materialize with `K0`;
in N goroutines each `kernel.New()` → `LoadModulePackage` → `SynthesizeRelease` →
`Compile(rel, sharedMP)` under `-race`. For v0.17, use an **in-memory `registrytest`
catalog** instead of the published `@v0.4.0` (which won't parse). On v0.16 this fails
at `compile.Match`; on v0.17 (+ compatible catalog) it should be race-clean.

---

## 7. ADR-002 (verbatim — recreate as `library/adr/002-*.md` on the new branch)

**Title:** Concurrent rendering against a shared materialized platform (CUE v0.17).
**Status:** Proposed — gates the recontract; not yet implemented.

**Decision:** Adopt the v0.17 concurrent-render model: per-goroutine Kernels, one shared
read-only `*MaterializedPlatform`, no mutex, no re-materialize. Production change is the
narrow `opm/compile` context-threading described in §5 above. Rejected: shared Kernel +
mutex (serializes); per-goroutine re-materialize (defeats materialize-once).

**Consequences — Positive:** concurrent parallel rendering against one materialized
platform; no operator mutex; platform reused not rebuilt; kernel change small/contained.
**Negative/Trade-off:** pins alpha CUE v0.17 (workspace on v0.16.1); ~2.5× saturates ~4
cores (allocator-bound, sub-linear); published catalogs must be re-published for v0.17;
the raw-CUE keystone proves the primitive, kernel-on-v0.17 end-to-end is unconfirmed
pending a v0.17-compatible catalog. **Decoupling:** operator never waits on this.

---

## 8. Where this sits in the bigger picture

- This spike is the library-side concurrency question that branched off the operator
  rewrite (see `KERNEL-MIGRATION-NOTES.md`). It is **independent** of the operator's two
  slices (render-rewrite + Platform CRD).
- The operator can proceed entirely on v0.16 + mutex. The recontract is a *later*
  library optimization that removes that mutex.
- Next-session entry points:
  1. New library branch → recreate ADR-002 (§7) + recontract change → implement §5 when
     the §5 preconditions hold.
  2. Until then, operator render path uses a mutex (a ~10-line reversible concession).
