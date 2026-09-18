# Design: platform-inventory-gate

## Context

See `proposal.md` § Why. The decisions are `enhancements/0015` D5 (comparable predicates are refused at platform-package generation, not arbitrated) and D18 (an unfulfilled contract is a non-gating report; only over-subscription refuses generation), both placed at 0019 D6's cold path: the Platform reconciler's generate-and-build step.

Current state, measured 2026-09-18:

- `PlatformReconciler.Reconcile` (`internal/controller/platform_controller.go`) derives the closure, generates the module, writes it under the identity's directory, builds it with `Kernel.AcquirePlatformFromDir`, then `Store.SetGenerated`, prunes, records the effective registry and marks `Ready=True/Generated`. Nothing reads `Contracts()`. A skip path returns early when the store already holds the tuple's identity and its directory exists.
- `failReconcile` sets `Ready=False` with a reason and message through `status.MarkStalled`, stamps `observedGeneration` and `operatorVersion`, emits the warning event only on transition (reason or message change), requeues on `StalledRecheckInterval` (30 minutes) or the transient interval when the classified error is a network timeout, and leaves the store untouched. `recordEffectiveRegistry` is called only where the held package is the one under that identity, so a failure leaves `status.packageIdentity` and `status.registry` describing the last good package.
- `patchStatus` declares the owned conditions `Ready`, `Reconciling`, `Stalled`.
- `internal/status/conditions.go` has no `ContractsFulfilled` condition and no inventory reason; every refusal in the package carries a reason of its own.
- Library `v1.0.0-alpha.32` (`opm/platform/contracts.go`): `ContractInventory{DefinedBy, RequiredBy, Unfulfilled, OverSubscribed, Comparable []ComparablePredicates{Broader, Narrower, Contracts}, Fulfilled, Routable, Discriminated}`; `Contracts()` errors naming a missing field. The operator pins alpha.31.
- Tests: `platform_controller_test.go` builds a kernel from `CUE_REGISTRY` and skips without one (`buildKernelOrSkip`, forced by `OPM_TEST_REGISTRY_FORCE=1` in CI); the registry-backed specs subscribe `testCatalogPath()` at `fixtures.CatalogVersion()` (default `4.0.1`, `OPM_TEST_CATALOG_VERSION` in seeded CI). `platform_failure_test.go` exercises `failReconcile` directly with no registry. No published catalog pair is over-subscribed or undiscriminated, and `catalogs/opm` `4.0.1` predates the contract maps (`4.1.0`), so the default test platform's inventory is empty.
- `docs/RENDERING.md` documents the Platform reconciler and the `PlatformNotReady` instance state ("automatic once the Platform is `Generated`").
- Spike (section 1, measured 2026-09-18 on library `v1.0.0-alpha.32` against GHCR): the test platform subscribing `opmodel.dev/catalogs/opm@v4` at `4.0.1` builds, and `Contracts()` returns with no error and an entirely empty inventory: `len(DefinedBy)` 0, `len(RequiredBy)` 0, `Unfulfilled` `[]`, `OverSubscribed` `[]`, `Comparable` `[]`, `Fulfilled`, `Routable` and `Discriminated` all true. The generated module's core pin reads `v2.0.0-alpha.10` (`schema.DefaultSchemaVersion()`, the library's own), with no operator constant involved. So the healthy live spec sees the vacuous case — `ContractsFulfilled=True` reason `NoContractsDefined` — at the default pin, which is exactly why the live spec asserts against the held platform's inventory rather than a literal, and why the three condition states are pinned by the table tests.

## Goals / Non-Goals

**Goals**

- The two refusals 0015 places at generation exist, name the platform-level facts, and preserve everything a failed build preserves.
- The D18 report exists as a condition that cannot be mistaken for a gate, and its vacuous case is visible.
- Every message is deterministic, so `failReconcile`'s transition-gated event fires once per distinct verdict.

**Non-Goals**

- D14 (no general-stage wait exists to exclude the registration from).
- A render-time tripwire on `discriminated` in the render path.
- Retiring the kernel's render-time over-subscription gate.
- A fixture catalog pair for the refusals (see Research & Decisions).
- Any change to the claim reconciler: it judges claims; this gate judges the package.

## Decisions

### Where the gate sits

Between the build and the store write, so a refused package is never the one renders consume:

```go
p, err := r.Kernel.AcquirePlatformFromDir(ctx, dir)
if err != nil {
	return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, fmt.Sprintf("building platform module: %v", err))
}
inv, err := p.Contracts()
if err != nil {
	return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, fmt.Sprintf("reading the platform's contract inventory: %v", err))
}
if reason, msg, refused := inventoryRefusal(inv); refused {
	return r.failReconcile(ctx, patcher, &plat, reason, nil, msg)
}
r.Store.SetGenerated(...)
// ... prune, recordEffectiveRegistry ...
setContractsFulfilled(&plat, inv)
status.MarkReadyWithReason(&plat, status.GeneratedReason, ...)
```

`failReconcile` is reused unchanged: a refusal is not transient (`classifyErr` nil), so it requeues on the stalled interval, and the fix (a Platform edit or a claim change) already wakes the reconciler through the generation-change predicate and the claim watch. The refused directory is left on disk; the next successful generation's `Prune` removes it, as it does every superseded directory.

### The two pure functions

`internal/controller/platform_inventory.go`:

```go
// inventoryRefusal decides whether a built platform may be recorded (0015 D5, D18).
func inventoryRefusal(inv *platform.ContractInventory) (reason, msg string, refused bool)

// contractsFulfilled is the non-gating D18 report as a condition.
func setContractsFulfilled(plat *releasesv1alpha1.Platform, inv *platform.ContractInventory)
```

`inventoryRefusal` returns `OverSubscribedContracts` when `!inv.Routable`, `ComparablePredicates` when routable and `!inv.Discriminated`, and both findings in the message when neither holds. Every list is sorted before printing (contract FQNs, `RequiredBy` FQNs, comparable rows by broader then narrower, shared contracts), because `failReconcile` gates the warning event on message equality and core's list order is comprehension order. Message shapes:

```
platform is not routable: 1 over-subscribed contract; a platform package cannot be generated until one competing catalog is disabled or its claim removed:
  opmodel.dev/catalogs/opm/traits/backup@v1alpha1 (defined by opmodel.dev/catalogs/opm@v4) required by opmodel.dev/catalogs/k8up/transformers/schedule@1.0.0, opmodel.dev/catalogs/velero/transformers/schedule@1.0.0

platform is not discriminated: 1 comparable transformer pair; every component the narrower transformer matches is also matched by the broader one, so both would render (enhancement 0015 D5):
  testing.opmodel.dev/cat2/transformers/mirror@0.2.0 (broader) and testing.opmodel.dev/cat/transformers/deployment@0.1.0 (narrower) over testing.opmodel.dev/cat/resources/container@v1
```

### The condition

`status.ContractsFulfilledCondition = "ContractsFulfilled"` with reasons `UnfulfilledContractsReason`, `ContractsFulfilledReason`, `NoContractsDefinedReason`. Written on the fresh-build path after `SetGenerated` and on the skip path from `held.Platform.Contracts()`, never on a refusal or a failure (the condition describes the package renders consume, like `status.registry`). Added to `patchStatus`'s owned conditions so the serial patcher diffs and clears it as the reconciler's own. `NoContractsDefined` is `True`: a raw-passthrough-only platform legitimately defines nothing, and an `Unknown` there would read as a fault forever; the reason is what makes the vacuous case visible (0015 `06-operational.md`).

On the skip path, if `held.Platform.Contracts()` errors, the held record is treated as stale and the reconcile falls through to regeneration, where the read error surfaces as `BuildFailed`.

### Reconcile phase impact

| Phase | Change |
| --- | --- |
| Source (closure derivation) | none |
| Generate and write | none |
| Build | none |
| Gate (new) | `Contracts()` read; `inventoryRefusal` before `SetGenerated` |
| Store and prune | refused packages never enter the store; their directory waits for the next successful prune |
| Status | two `Ready=False` reasons; `ContractsFulfilled` on success paths; owned-conditions list |
| Render path | untouched; consumes the last good package or reports `PlatformNotReady` |

### Files touched

| File | Change |
| --- | --- |
| `go.mod`, `go.sum` | library `v1.0.0-alpha.32` |
| `internal/status/conditions.go` | condition type and four reasons |
| `api/v1alpha1/platform_types.go` | `PlatformStatus.Conditions` doc comment |
| `config/crd/bases/opmodel.dev_platforms.yaml`, `dist/install.yaml` | regenerated |
| `internal/controller/platform_inventory.go`, `platform_inventory_test.go` | the two functions and their table tests |
| `internal/controller/platform_controller.go` | the gate, the condition on both success paths, owned conditions |
| `internal/controller/platform_controller_test.go`, `platform_failure_test.go` | refusal specs through the helper; condition on the live spec |
| `docs/RENDERING.md` | the refusals and the condition; the `PlatformNotReady` remedy |

## Research & Decisions

### How the refusals are tested without a refusing catalog pair

**Context**: no published catalog pair is over-subscribed or undiscriminated, and the operator's fixtures are modules, published through the cli's gated pipeline and seeded into PR CI by `hack/fixtures.sh`, a script shared byte-for-byte with the cli.
**Explored**: (a) a fixture catalog pair under `testing.opmodel.dev/catalogs/operator/*`, extending the fixture pipeline to catalogs in both repos; (b) an injectable inventory reader on the reconciler; (c) exercising the refusal through the reconciler's helper with hand-built `platform.ContractInventory` values, the pattern `platform_failure_test.go` already uses for `failReconcile`, plus table tests on the two pure functions.
**Decision**: (c).
**Rationale**: the novel behaviour is the verdict, the wording and what a refusal leaves untouched; all three are exercised by (c) against the real API server. (a) adds a second fixture kind to a shared pipeline for two messages, and (b) adds a seam whose only caller is a test. The live-registry specs keep covering the wiring on the healthy platform, and the parity harness in the library keeps the shipped catalog discriminated, so the healthy path is the one the fixtures exercise.

### The condition asserts against the held package, not a literal

**Context**: the default test catalog pin (`4.0.1`) predates the contract maps, so the healthy test platform's inventory is empty; seeded CI may pin a newer build whose inventory defines contracts and leaves the backup traits unfulfilled.
**Explored**: branching the assertion on `fixtures.CatalogVersion()`; asserting the reason expected for each pin; asserting the condition against `Contracts()` read off the store's held platform.
**Decision**: read the held platform's inventory in the spec and assert the condition matches it (reason and message), with the three reason states covered by the table tests.
**Rationale**: the spec then holds at whichever build the fixtures pin, and a fixture bump cannot silently turn a real report into a vacuous one without the table tests still pinning every wording.

### Over-subscription is reported first when both hold

**Context**: one `Ready` condition carries one reason; a platform can be both over-subscribed and undiscriminated.
**Explored**: two reasons in one condition (impossible); a combined reason; routing first with both findings in the message.
**Decision**: routing first, both findings in the message.
**Rationale**: 0015 D18 fixes over-subscription as the generation refusal and D5 joins it; an operator fixing the routing problem sees the discrimination problem in the same message rather than after the next reconcile, and a reason of its own per refusal is kept for the single-finding case, which is the common one.

### Refusal leaves the condition and the registry describing the last good package

**Context**: a refused inventory has its own `Unfulfilled` list; writing it would describe a package no render consumes.
**Explored**: writing `ContractsFulfilled` from the refused inventory; leaving it.
**Decision**: leave it, exactly as `recordEffectiveRegistry` is left on failure.
**Rationale**: `status.registry`, `status.packageIdentity` and `ContractsFulfilled` describe one package, the one renders consume; splitting them across two packages makes the status lie in one direction or the other. The refusal's own facts live in the Ready message.

### The refused directory is not pruned on refusal

**Context**: a refused generation has written a module directory the store never records.
**Explored**: pruning immediately with the held identity plus leases as the keep set; leaving it for the next successful prune.
**Decision**: leave it.
**Rationale**: `Prune` runs after every successful generation with the exact keep set and already removes every unrecorded directory; a second prune site adds a path to test for disk hygiene that costs a directory per refused tuple, and the manager's boot `Reset` empties the root anyway.

### Section 1 is a spike

**Context**: design.md carries one unverified assumption: that the suite is green on library alpha.32 (the generated module's core pin moves to alpha.10 with it) and that the test platform's inventory reads as the Context describes.
**Decision**: section 1 bumps the pin, runs the gates, and measures `Contracts()` on the test platform before any verdict is written.

## Risks / Trade-offs

- [A cluster whose platform is over-subscribed or undiscriminated today stops generating after the upgrade] → intended: its renders failed or doubled already; the Ready message names the catalogs to fix, and the last good package (if any) keeps serving until then.
- [The core pin moving to alpha.10 with the library bump changes generated bytes] → the pin is the library's, not the operator's (`Core pin follows the library`); the live specs re-verify the build in section 1.
- [Message churn re-fires the warning event] → every list is sorted; the table tests pin the wording and compare two orderings of the same inventory to one message.
- [Seeded CI pins a catalog build whose inventory is non-empty] → the live spec asserts against the held platform's inventory, not a literal.
- [Reading `Contracts()` on every reconcile] → measured by core at roughly +0.01 s for the shipped catalog; the skip path reads it off the already built value.

## Migration Plan

One PR, three sections, squash title `feat(controller): refuse generation on an over-subscribed or undiscriminated platform`. release-please cuts the next operator pre-release. Rollback before release is a revert; after, a later release. No CRD schema change, so no stored-object migration; the new condition appears on the next reconcile after upgrade and the new reasons only where a platform was already unusable.

## Open Questions

None.
