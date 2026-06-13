# Design — remove-bundlerelease-scaffolding

## Context

`BundleRelease` was scaffolded ahead of any bundle design (ADR-007 sketched it as an orchestrator over child `ModuleRelease`s). Today it consists of: a full CRD type (`api/v1alpha1/bundlerelease_types.go`), an empty `BundleReleaseReconciler` (logs and returns), a 4-line placeholder in `internal/reconcile/bundlerelease.go`, config surface (CRD base, sample, three aggregated RBAC roles), and references across ADRs, `CONSTITUTION.md`, `CLAUDE.md`, and three openspec specs.

Meanwhile the schema source of truth has no bundle concept: neither `core/` nor `library/` defines `#BundleRelease`; the kernel accepts exactly `Module`, `ModuleRelease`, `Platform`. The Release pipeline's kind-detection branch (`internal/render/kernel_release_renderer.go`) speculatively labels any wrong-kind package "BundleRelease" — encoding a *second*, competing bundle future (Release-renders-bundles) alongside ADR-007's orchestrator model. Both are unbacked by design.

No `kind: BundleRelease` resource exists in this repo's samples-consuming environments or anywhere in the workspace (`releases/`, `modules/`, `opm-kind-demo/`).

## Goals / Non-Goals

**Goals**

- Delete all BundleRelease API, controller, reconcile, config, and test surface.
- Preserve the wrong-kind safety gate in the Release render path with identical semantics (`ErrUnsupportedKind` → `Ready=False`, reason `UnsupportedKind`, `Stalled=True`), stripped of speculative bundle naming.
- Leave docs/ADRs/specs describing only what exists; record *why* bundle work is deferred.
- Fix doc claims that are stale independently of this change where the same files are touched.

**Non-Goals**

- Any bundle redesign or commitment to a future bundle model (orchestrator vs inline rendering stays an open question, deliberately).
- `ModuleRelease`/`Release` consolidation (explored separately; decided against for now).
- Behavior changes to apply/prune/inventory/status.

## Decisions

### D1: Generalize the kind-detection gate instead of deleting it

The gate in `KernelReleaseRenderer` (gating `Kernel.LoadReleasePackage` to `#ModuleRelease`, mapping `loaderfile.ErrWrongKind` to `ErrUnsupportedKind`) is a safety boundary, not bundle plumbing. It stays. Only the `KindBundleRelease` constant, the "BundleRelease is the only other release kind" comment (factually wrong — the kernel also knows `Module` and `Platform`), and the bundle-specific error string are removed in favor of a generic unsupported-kind result.

*Alternative considered*: delete the branch and let `ErrWrongKind` surface raw — rejected; the reconciler's `UnsupportedKind` stall classification and its test coverage are worth keeping stable.

### D2: Supersede ADR-007, don't delete it

ADR-007 records real thinking about bundle orchestration. Its status changes to Superseded with a short note: bundle orchestration is deferred pending design; the speculative scaffolding was removed so the codebase only describes what exists. This preserves the orchestrator-vs-inline question for the future bundle design instead of silently losing it.

*Alternative considered*: delete the ADR — rejected; ADRs are the record of *why*, including reversals.

### D3: Hand-edit `PROJECT` as a deliberate exception

Repo rule says never hand-edit `PROJECT`, but Kubebuilder has no `delete api` command; removing the BundleRelease resource entry by hand is the only mechanism. The edit is confined to deleting that one resource block, called out explicitly in its own task/commit.

### D4: Remove `ModuleStatusSummary` with the CRD

`ModuleStatusSummary` in `common_types.go` exists only for `BundleReleaseStatus.Modules`. Keeping an unused exported API type would recreate the same confusion this change removes. If a future bundle design needs a child-status summary, it will define one against its actual shape.

### D5: Fix adjacent stale doc claims in the same pass

`CONSTITUTION.md` ("`internal/source/` retained for BundleRelease" — it is used by `Release` today) and `CLAUDE.md` (CRD list says "ModuleRelease + BundleRelease", omitting `Release` and `Platform`) are corrected here because leaving them half-updated after deleting BundleRelease would make them *more* misleading, not less. Scope is limited to lines this change invalidates.

## Risks / Trade-offs

- [Orphaned CRD on existing clusters] `kubectl apply` of a regenerated `dist/install.yaml` never deletes the old CRD → documented one-liner: `kubectl delete crd bundlereleases.releases.opmodel.dev`. Demo/kind clusters are throwaway; no production consumers exist.
- [Losing wrong-kind test coverage during generalization] `TestKernelReleaseRenderer_BundleReleaseUnsupported` and the `release_controller_test.go` stub-error case are renamed/generalized, never deleted; verified by `task dev:test`.
- [Future bundle work loses a starting point] Mitigated by D2 (ADR-007 superseded, not erased) and by git history; scaffolding this thin carries no design information beyond the ADR.

## Migration Plan

1. Code + config deletions and edits land as one small change (deletion-heavy, ~310 LOC of Go).
2. Regenerate: `task dev:manifests dev:generate` (CRDs, RBAC role, DeepCopy), `task operator:installer` (`dist/install.yaml`).
3. Validation gates: `task dev:fmt dev:vet dev:lint dev:test`.
4. Rollback: revert the commit; `task dev:manifests dev:generate operator:installer` restores generated surface.
5. Cluster note (release notes / CHANGELOG): existing installs keep an inert `bundlereleases.releases.opmodel.dev` CRD; remove with `kubectl delete crd bundlereleases.releases.opmodel.dev`.

## Open Questions

None blocking. The deliberate non-decision: what bundles become (orchestrator CRD, Release-rendered, or something else) — explicitly deferred to a future design with a real `#Bundle`/`#BundleRelease` schema in `core/`.
