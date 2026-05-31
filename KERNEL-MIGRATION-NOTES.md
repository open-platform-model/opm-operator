# Operator → Library Kernel Migration — Exploration Notes

> **Status:** temporary working document (explore-phase capture, not a design spec).
> **Date:** 2026-05-30
> **Scope:** what it takes to rewrite `opm-operator` to run 100% on the
> `open-platform-model/library` kernel and nothing else.
>
> Delete or promote into an enhancement slice once the Platform-source decision
> (see §5.0) is made.

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

**The one genuine gap is not a missing method** — it's that the operator has **no
source for a `#Platform`**. Today it loads a provider composition from a flag.
Post-0001 the kernel needs a `*platform.Platform` to `Materialize`. That's a
design decision (§5.0), not a library shortfall.

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
provider injection with `Kernel.Materialize(platform)` → `*MaterializedPlatform`
(+ `materialize/cache` keyed on platform/CR generation).

### 5.2 Replace render core
Delete `pkg/{provider,render,module,loader,validate}` + `internal/{catalog,synthesis}`.
Rewire `internal/render` to `Kernel.SynthesizeRelease/ProcessModuleRelease →
Match → Compile`. **Stage this** (synthesis+process → match/compile →
materialize); the library constitution has a hard small-batch gate and this is a
5-package deletion + rewire.

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
   └─▶ PlatformReconciler: NewPlatformFromValue(spec → cue.Value)
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

## 9. Kernel concurrency (spike DONE → see dossier)

The library-side concurrency question (can renders run concurrently against one
shared materialized platform?) was spiked and **decided: adopt the CUE v0.17
per-goroutine-Kernel + shared-read-only-platform model.** Full results, exact
numbers, the recontract plan, and reproduction code are in
[`KERNEL-CONCURRENCY-SPIKE-RESULTS.md`](./KERNEL-CONCURRENCY-SPIKE-RESULTS.md).

Bearing on the operator: **none on the critical path.** The operator ships on CUE
v0.16 with a render-path **mutex** (a ~10-line reversible concession), and drops
it only when the library recontract lands. v0.17 adoption is blocked on a stable
v0.17 release + re-published catalogs, so do not couple the operator rewrite to it.
