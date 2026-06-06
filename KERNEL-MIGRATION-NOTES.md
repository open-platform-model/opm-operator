# Operator → Library Kernel Migration — Exploration Notes

> **Status:** temporary working document (explore-phase capture, not a design spec).
> **Date:** 2026-05-30 — library status refreshed 2026-06-06 (see §0).
> **Scope:** what it takes to rewrite `opm-operator` to run 100% on the
> `open-platform-model/library` kernel and nothing else.
>
> The Platform-source decision (§5.0) is **resolved** — see §8. Remaining purpose
> of this note: fold into the operator's enhancement slice(s) under 0001.

## TL;DR

The operator **does not import the library kernel at all today**. It carries a
complete, parallel, **pre-0001** fork of the entire OPM runtime (~2,106 LOC under
`pkg/*` plus `internal/{catalog,render,synthesis}`), built directly on
`cuelang.org/go/cue`. `grep open-platform-model/library` across the operator
returns **zero hits**.

Worse than "needs a port": the fork is built on the mental model that
enhancement 0001 **deleted**. The operator's `Provider` (a CUE transformer
registry loaded from a static "catalog composition module" at startup) is the
`#defines`-era shape. The kernel replaced that with `#Platform` registry
subscriptions resolved against OCI at `Materialize` time.

So this is an architecture migration, not a method-by-method swap.

## 0. Library status since this note (updated 2026-06-06)

This note was first captured 2026-05-30. Four library OpenSpec changes have since
landed (all archived under `library/openspec/changes/archive/`); the body below
is annotated where they change a conclusion, but the headline deltas are:

- **`add-platform-synth`** (2026-06-06) — `synth.Platform(ctx, PlatformInput)` +
  `Kernel.SynthesizePlatform(ctx, PlatformInput) (*platform.Platform, error)`
  now build a validated `#Platform` from **typed in-memory inputs**, stopping
  before `Materialize`. This is the first-class library answer to the "operator
  has no source for a `#Platform`" gap (§3): the operator maps its `Platform`
  CRD spec → `synth.PlatformInput` → `SynthesizePlatform` → `Materialize`, with
  no hand-rolled CUE text. The §8.2 `NewPlatformFromValue(spec → cue.Value)`
  step is superseded by this typed path.
- **`concurrent-render-recontract`** + **`enable-concurrent-render-v017`**
  (2026-05-31 / 2026-06-01) — concurrency is **shipped**, the library is pinned
  to CUE `v0.17.0-alpha.1`, and one materialized platform is safe to share
  read-only across per-goroutine Kernels. The operator no longer needs the
  interim render mutex. See §9 (rewritten).
- **`remove-library-catalog`** (2026-05-31) — the OPM core catalog source and
  its publish pipeline are **deleted from the library**; the catalog lives in the
  `catalog_opm` repo (`opmodel.dev/catalogs/opm@v0`, latest `v0.4.0` on GHCR).
  The library only *consumes* the published catalog in test fixtures. The
  operator likewise resolves catalogs from the registry at `Materialize` time —
  there is no library-vendored catalog to depend on.

Net: **no library blocker remains for the operator rewrite.** Every kernel
primitive the operator needs — platform construction, materialize, concurrent
render — is shipped. What's left (§8) is operator-side: the `Platform` CRD +
reconciler and the render-core swap.

## 1. Current operator architecture (pre-0001)

```
cmd/main.go:232  →  catalog.LoadProvider(catalogPath, providerName)  →  one *provider.Provider
                    injected into every reconciler at startup

catalog composition module ──load──▶ Provider{ Data: cue.Value w/ #transformers,
(a "providers" map on disk)          Metadata: {name,version} }
                                              │
ModuleRelease CR ──synthesis(text/template)──▶ temp CUE module ──▶ *module.Release
                                              │
                          pkg/render: Finalize → Match(Go) → Execute(CUE)
                                              │
                                   []*core.Resource ──▶ apply / inventory
```

- Provider loaded **once at startup** from `--catalog-dir` + `providerName`,
  injected into `ModuleRelease`/`Bundle` reconcilers.
- `internal/synthesis` generates a temp CUE module via `text/template`, with a
  **hardcoded** `CatalogVersion = "v1.3.4"` pin.
- CRDs: `ModuleRelease`, `BundleRelease`. **No Platform CRD, no Platform artifact.**

## 2. Kernel architecture (post-0001)

```
#Platform{ #registry: subscriptions } ──Kernel.Materialize──▶ *MaterializedPlatform
                                        (#composedTransformers + #matchers)
                                              │
values ──Kernel.SynthesizeRelease / ProcessModuleRelease──▶ *module.Release
                                              │
            Kernel.Finalize → Kernel.Match → Kernel.Compile
                                              │
                                *kernel.CompileResult → []*core.Compiled
```

Kernel public surface (all on `*kernel.Kernel`): `LoadModulePackage` /
`LoadReleasePackage` / `LoadPlatformPackage`, `NewPlatformFromValue` /
`NewModuleFromValue` / `NewReleaseFromValue`, `Materialize`,
`SynthesizeRelease`, `ProcessModuleRelease`, `ValidateConfig*` /
`ValidateRelease*`, `Finalize`, `Match`, `Plan`, `Compile`. Terminal output is
`opm/core.Compiled`.

Lifetime contract (from `library/CLAUDE.md`): **long-running consumers MUST keep
one Kernel alive** — one `*schema.Cache` per Kernel, schema fetched once on
first use. `Materialize` holds no cache; wire `opm/materialize/cache` (LRU +
`Key(*platform.Platform)`) keyed on CR generation.

## 3. Q1 — Does the kernel have everything the operator needs?

For the render pipeline: **yes, essentially complete.** Mapping:

| Operator (homegrown) | Kernel equivalent | Status |
|---|---|---|
| `pkg/loader` (LoadModule/Provider) | `Kernel.LoadModulePackage/LoadReleasePackage/LoadPlatformPackage` + `helper/loader/file` | covered |
| `internal/synthesis` (text/template) | `Kernel.SynthesizeRelease(synth.ReleaseInput)` — Module, Name, Namespace, **Values**, Labels/Annotations | covered (kernel injects values; template version is cruder) |
| `pkg/module` + `ParseModuleRelease` | `Kernel.ProcessModuleRelease` / `NewReleaseFromValue` | covered |
| `pkg/validate` | `Kernel.ValidateReleaseValues{,Partial,Detailed}` / `ValidateConfig*` | covered |
| `pkg/provider` + `internal/catalog` | `opm/platform` + `opm/materialize` + `Kernel.Materialize` → `*MaterializedPlatform` | **model change**, not a port |
| `pkg/render` Finalize/Match/Execute | `Kernel.Finalize` / `Kernel.Match` / `Kernel.Compile` | covered |
| `pkg/core` Compiled/Resource | `opm/core.Compiled` (terminal); operator keeps own `core.Resource`+`Identity` adapter | by design |
| `pkg/errors` | `opm/errors` (alias `oerrors`) | covered |
| caching | `opm/materialize/cache` (LRU + `Key`) | available; wire to CR generation |

**The one genuine gap is not a missing method** — and as of `add-platform-synth`
(2026-06-06) it is **no longer a library gap at all**. The operator still has
**no *source* for a `#Platform`** (today it loads a provider composition from a
flag), but the library now ships `Kernel.SynthesizePlatform(ctx, PlatformInput)`
to turn typed inputs into a validated `*platform.Platform` ready to `Materialize`
(see §0). What remains is purely operator-side: where the typed input comes from
— resolved as the `Platform` CRD in §8, not §5.0.

## 4. What stays operator-side (correctly outside the kernel)

The library explicitly states adapters wrap each `*core.Compiled` with a
platform-specific `core.Resource` filling `core.Identity`. So these **stay**:

- `pkg/core/convert.go`, `pkg/core/resource.go` — the `core.Compiled → core.Resource/Identity` K8s adapter (repoint at the library's `core.Compiled`).
- `pkg/resourceorder/weights.go` — K8s apply-ordering (GVK weights).
- `internal/{apply,inventory,status,reconcile,source}` — server-side apply, inventory, status conditions, Flux source fetch, reconcile loop.
- Bundle expansion (`internal/render/bundle.go` is currently `type BundleRenderer any` — a stub).

## 5. Q2 — Work plan (dependency-ordered)

### 5.0 BLOCKING DECISION — where does `#Platform` come from?

Settle this first; everything downstream depends on it.

- **(a) Platform CRD** (cluster-scoped) the operator watches → materialize + cache by generation.
- **(b) Platform artifact loaded at startup** from config/OCI — closest to today's `--catalog-dir`, smallest behavioral change.
- **(c) Platform ref on Bundle/ModuleRelease spec.**

Check `enhancements/0001/{02-design.md,07-next-steps.md}` first — a decision may
already exist there.

### 5.1 Add library dep + long-lived Kernel
`cmd/main.go`: construct one `*kernel.Kernel` (`WithRegistry`/`WithLogger`/
`WithSchemaLoader`), keep it alive. Replace startup `catalog.LoadProvider` →
provider injection with `Kernel.SynthesizePlatform(platformSpec)` →
`Kernel.Materialize(platform)` → `*MaterializedPlatform` (+ `materialize/cache`
keyed on platform/CR generation).

### 5.2 Replace render core
Delete `pkg/{provider,render,module,loader,validate}` + `internal/{catalog,synthesis}`.
Rewire `internal/render` to `Kernel.SynthesizeRelease/ProcessModuleRelease →
Match → Compile`. **Stage this** (synthesis+process → match/compile →
materialize); the library constitution has a hard small-batch gate and this is a
5-package deletion + rewire.

This deletion also clears the two pre-existing `staticcheck` SA1019
(`cue.Value.Context()` deprecation) findings at `pkg/render/module_renderer.go:85`
and `pkg/render/process_modulerelease.go:26`. They were left untouched by the
`wire-library-kernel` slice (out of scope — that slice does not modify the legacy
fork; `//nolint` rejected per the events-api migration precedent) and are resolved
by removing the file, not by migrating soon-to-be-deleted code.

### 5.3 Adapt output
Repoint `pkg/core/convert.go` to consume the library's `*core.Compiled`,
producing the existing operator `core.Resource`.

### 5.4 Keep & re-point K8s tail
`internal/{apply,inventory,status,reconcile}`, `pkg/resourceorder`,
`internal/source` consume `[]*core.Resource` — minimal change beyond type repoint.

### 5.5 BundleRelease
Decide operator-side expansion vs kernel helper (likely operator-side). Flesh out
`internal/render/bundle.go`.

### 5.6 Tests
Controller tests inject a stub `ModuleRenderer` — that boundary survives; repoint fakes. Repoint envtest/integration.

## 6. Flags to resolve before coding

1. **§5.0 Platform-source decision is the linchpin.** Everything depends on it.
2. **Scope/batching** — multi-package deletion + rewire; trips the library's hard
   small-batch gate. Should be an enhancement slice (under 0001 or its own) with
   per-stage OpenSpec changes in this repo, not one PR.
3. **`synthesis.CatalogVersion = "v1.3.4"`** — hardcoded build-time pin in the
   text-template synthesizer. `Materialize` resolves catalogs from `#registry`
   subscriptions via live OCI, so this constant and the "controller built against
   catalog vX" coupling dissolve. Confirm live-registry resolution is the
   intended direction.

## 7. Open questions back to the author

- Is the Platform-source decision already made (0001 doc / operator ADR), or open?
- Frame as an enhancement slice under `enhancements/0001`, or still at
  feasibility/sizing stage?

> Both resolved — see §8.

## 8. Platform registration — DECIDED

How the operator obtains and registers the `#Platform` it materializes.
Decisions below are locked; this section supersedes the open questions in §5.0
and §7.

### 8.1 Decisions

- **One global Platform per cluster.** No per-tenant / per-namespace platforms,
  no layered-inheritance model (considered, deferred as too complex for now).
- **Source = a cluster-scoped `Platform` CRD.** Not a checked-in CUE file, not a
  ConfigMap, not a startup flag, not an embedded default. The CRD's `spec` is a
  near-1:1 projection of `#Platform` (`type` + `registry` path-keyed
  subscriptions with `{enable, filter{range,allow,deny}}`).
- **Platform-admin owned.** Tight RBAC; tenants never touch it. Mirrors the
  ServiceAccount/RBAC split in `docs/TENANCY.md` (platform = admin artifact,
  release = tenant artifact).
- **Inert until the Platform CR exists.** With no `Platform` CR applied, the
  operator does **nothing** — no embedded default, no fallback. `ModuleRelease` /
  `BundleRelease` reconciles no-op to a blocked condition
  (`PlatformNotReady` / `NoPlatform`) and apply nothing to the cluster.
- **Singleton via fixed name + CEL.** The only permitted name is `cluster`,
  enforced declaratively by an `x-kubernetes-validations` rule on the CRD root.
  Name uniqueness (cluster-scoped) ⇒ at most one object can exist. No webhook.

  ```go
  // +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="Platform is a cluster singleton; the only permitted name is 'cluster'"
  type Platform struct {
      metav1.TypeMeta   `json:",inline"`
      metav1.ObjectMeta `json:"metadata,omitempty"`
      Spec   PlatformSpec   `json:"spec,omitempty"`
      Status PlatformStatus `json:"status,omitempty"`
  }
  ```

  (`metadata.name` is the one metadata field a CEL root rule may read.) The
  controller also reconciles **only** `cluster` as defense-in-depth.
- **`type` field stays informational.** Not a selector; the matcher ignores it.
- **Binding is implicit.** Every release in the cluster materializes against the
  single platform. No `platformRef`, no selector.

### 8.2 Runtime model

```
no Platform CR in cluster
   └─▶ PlatformReconciler: nothing materialized
   └─▶ ModuleRelease/BundleRelease reconcile → no-op, condition
        PlatformNotReady, requeue. NOTHING applied to the cluster.

Platform CR `cluster` applied (platform-admin RBAC)
   └─▶ PlatformReconciler: Kernel.SynthesizePlatform(spec → *platform.Platform)
        → Kernel.Materialize → cache slot (keyed on .metadata.generation)
        → status: Materialized=True | MaterializeError surfaced on the CR
   └─▶ all releases re-enqueued → Kernel.Compile against materialized platform → apply
```

### 8.3 Implementation defaults

- **Cache:** a single slot keyed on the Platform CR's `.metadata.generation`.
  The library `materialize/cache` LRU is overkill for one platform; a guarded
  single slot suffices.
- **Fan-out:** controller watches the `Platform` CR; on generation change, swap
  the cache slot and re-enqueue all releases.
- **Failure surface:** `MaterializeError` (structured, kernel-emitted) →
  conditions on the Platform CR. Releases reflect blocked, pointing at the
  platform.
- **Startup:** controller boots with no platform; reconcilers gate every release
  on "is there a ready materialized platform?" No boot-time load, no
  `--platform-path`, no mounted file.

### 8.4 Deletion semantics

**Freeze, don't tear down.** Deleting the `Platform` CR stops reconciliation and
flips releases to a blocked condition, but **already-applied workloads keep
running**. Deleting the platform is an admin "pause" action, not a cluster-wide
uninstall. Teardown remains owned by deleting the individual `ModuleRelease`.

### 8.5 Slice / branch shape

- **Two OpenSpec changes, one branch.** The same-branch decision dissolves the
  "interim platform source" problem — no bridge/scaffolding needed.
- **Layering:** the **Platform CRD change is the lower layer** (provides the
  platform); the **render-core rewrite sits on top** (consumes the materialized
  platform). Author the rewrite change assuming the CRD exists.

## 9. Kernel concurrency — LANDED (was: spike DONE)

The library-side concurrency question (can renders run concurrently against one
shared materialized platform?) is **resolved and shipped**, not just spiked. Two
OpenSpec changes landed it after this note was first written:

- **`concurrent-render-recontract`** (archived 2026-05-31; ADR-002 →
  Accepted) — the v0.16-safe half: the compile pipeline now threads the **caller
  Kernel's** `*cue.Context` through Finalize/Execute and reads the
  `*MaterializedPlatform` as read-only input, instead of funnelling every render
  through the platform's one context. `compile.NewModule` gained a leading
  `*cue.Context` param (kernel-internal caller only; the operator goes through
  `Kernel`, so it is unaffected).
- **`enable-concurrent-render-v017`** (archived 2026-06-01) — the enablement
  half: the library is now pinned to **`cuelang.org/go v0.17.0-alpha.1`**, the
  republished `core` / `catalogs/opm` parse under it, and a permanent concurrent
  `-race` regression test proves that one `*MaterializedPlatform`, materialized
  **once**, is safe to be read concurrently by N per-goroutine Kernels' `Compile`
  calls. The **Goroutine Safety Contract** + **MaterializedPlatform Output
  Shape** specs now assert this guarantee.

Bearing on the operator — **the interim render mutex is no longer needed.** The
earlier plan ("operator ships on CUE v0.16 with a render-path mutex, drop it when
the recontract lands") is overtaken: the recontract has landed. The operator's
materialize-once / render-many model (§8.3) can render concurrently against one
shared `*MaterializedPlatform` with **no render-path mutex from day one** —
provided it builds on the same CUE v0.17.x line as the library.

One caveat survives: the guarantee rests on an **alpha** CUE pin
(`v0.17.0-alpha.1`), not a stable release. The operator inherits that pin
transitively, and a future bump to a stable v0.17 is a shared maintenance event
across library + operator + cli. Full spike numbers and reproduction remain in
[`KERNEL-CONCURRENCY-SPIKE-RESULTS.md`](./KERNEL-CONCURRENCY-SPIKE-RESULTS.md).
