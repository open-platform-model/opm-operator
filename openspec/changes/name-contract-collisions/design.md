## Context

See proposal.md (Why) for the wrong wording. This change is C of the five-change set in `orchestration.md`: A (core) folds only single-definer keys and reports `collisions` and `collidingEntries`, with `routable` false while any exist; B (library) decodes them as `ContractInventory.Collisions` and `CollidingEntries`, refuses a colliding platform at render with `*errors.ContractCollisionsError` (and `*errors.NotRoutableError` as a catch-all), and moves `schema.DefaultSchemaModule` to A's tag. The interface C codes against is in `orchestration.md` § Interface B -> C, D; B may rename only by reporting it under `surface`.

State on `origin/main` (2e348a6, library `v1.0.0-alpha.35`, core pinned through the library's `DefaultSchemaModule` at `2.0.0-alpha.12`), read 2026-09-30:

- **Today a colliding platform never reaches the gate.** With core `2.0.0-alpha.12`, two enabled definers of one key conflict in `#contracts.definedBy` and `defined`, so `Kernel.AcquirePlatformFromDir` fails at the loader (library `opm/internal/loader/load.go:83-85`) and the reconciler reports `BuildFailed` with the CUE conflict (`platform_controller.go`, "building platform module: ..."). Measured by the change set's mapper on four colliding fixtures (0026 experiment 01 case A, experiment 06 control).
- **The generation gate.** `platform_controller.go:271` reads `p.Contracts()`; a read error is `BuildFailed`; `inventoryRefusal(inv)` (`platform_inventory.go:64-80`) refuses and `failReconcile` marks `Ready=False`, Stalled, emits a warning event on a message change, and requeues on the stalled interval. The package is not recorded.
- **The wording defect.** `inventoryRefusal` calls `overSubscribedFinding` whenever `!inv.Routable` and sets `OverSubscribedContractsReason`. Under A's core a collision-only platform reads `Routable: false` with `OverSubscribed` empty, so the message is "platform is not routable: 0 over-subscribed contracts; a platform package cannot be generated until one competing catalog is disabled or its claim removed:" followed by nothing.
- **Downstream of the gate.** The render path (`internal/reconcile`) and TransformerRegistration acceptance read only recorded packages. A refused package is never recorded, so neither sees a collision. `renderFailureReason` (`internal/reconcile/resolution.go`) would map B's new causes to `RenderFailed` through its default branch, with the kernel's message verbatim; nothing reaches it.
- **Reason documentation sites.** `internal/status/conditions.go:69-95`, `api/v1alpha1/platform_types.go:181-199` (the `PlatformStatus.Conditions` comment, which feeds the CRD description), `platform_controller.go:86` (reconciler doc), `docs/RENDERING.md:89-146`, `docs/site/diagnostics/operator-conditions.md:38-39`.

## Goals / Non-Goals

**Goals:**

- A colliding platform is refused with a reason and a message that name the colliding keys and the registry entries to disable.
- The routing refusal prints only the findings the inventory carries, and still fails closed on an unexplained `Routable: false`.
- The operator reads the collision report and counts nothing.

**Non-Goals:**

- The fold (A) and the render refusal (B).
- Side-by-side catalog majors. 0026:D9 makes them legitimate through per-resolution builds; this change is the interim safety net's operator face.
- Operator wording for a render refused with `ContractCollisionsError` or `NotRoutableError`: a colliding package is never recorded, so no render sees one (Decision 5).
- A registry-backed colliding Platform spec: no catalog published on the testing domain (or `opmodel.dev`) carries two majors sharing contract keys. The shape is pinned by hand-built inventories here and by B's parity tests against real CUE.
- The build-compatibility index (change E, `index-build-compat-by-major`, a separate operator change).
- Re-pinning the operator's published test fixtures or `config/samples` to A's core; that is the supervisor's workspace `task deps:pins:fixtures` and `task deps:update`.

## Decisions

### 1. A reason of its own, `ContractCollisions`

```go
// internal/status/conditions.go
// ContractCollisionsReason: Ready=False, the built platform's inventory
// reports contract keys that more than one enabled registry entry defines
// (two majors of one catalog sharing keys), so the platform is not routable
// ...; reported ahead of OverSubscribedContracts and ComparablePredicates.
ContractCollisionsReason = "ContractCollisions"
```

The existing reasons are split by remedy (the `conditions.go` comment: over-subscription is fixed by disabling a competing provider, a comparable pair by narrowing a predicate). A collision has a third remedy, disabling all but one major of a defining catalog, so it gets a third reason.

**Alternative.** Fold collisions under `OverSubscribedContracts` with a second finding: no new reason string, but the reason would then promise providers that do not exist, and a tool keyed on the reason could not tell "disable a provider" from "disable a major".

### 2. The gate, findings by list, fail closed

```go
func inventoryRefusal(inv *platform.ContractInventory) (reason, msg string, refused bool) {
	var findings []string
	if len(inv.Collisions) > 0 {
		findings = append(findings, collisionFinding(inv))
		reason = status.ContractCollisionsReason
	}
	if len(inv.OverSubscribed) > 0 {
		findings = append(findings, overSubscribedFinding(inv))
		if reason == "" {
			reason = status.OverSubscribedContractsReason
		}
	}
	if !inv.Routable && len(inv.Collisions) == 0 && len(inv.OverSubscribed) == 0 {
		// Core read the platform unroutable and named no row. Refuse anyway.
		findings = append(findings, unroutableFinding)
		reason = status.OverSubscribedContractsReason
	}
	if !inv.Discriminated {
		findings = append(findings, comparableFinding(inv))
		if reason == "" {
			reason = status.ComparablePredicatesReason
		}
	}
	if len(findings) == 0 {
		return "", "", false
	}
	return reason, strings.Join(findings, "\n\n"), true
}

const unroutableFinding = "platform is not routable; the contract inventory names no over-subscribed or colliding contract, " +
	"so a platform package cannot be generated"
```

Two gates, and why each:

- **A non-empty `Collisions` refuses whatever `Routable` reads.** Core computes `routable` false whenever a collision exists, so on a consistent inventory the two agree. On an inconsistent one the list wins, because a collision blinds `definedBy`, `requiredBy` and `comparable`: nothing else in the inventory can be trusted to have caught the problem.
- **`OverSubscribed` decides whether the over-subscription finding prints**, and `Routable` stays the fail-closed backstop. An unroutable inventory with no row keeps today's reason (`OverSubscribedContracts`, the routing reason) but a message that does not claim "0 over-subscribed contracts". Under A's core this fires on no platform; it guards a future core term in `routable`, the same reason B adds `NotRoutableError`.

A non-empty `OverSubscribed` with `Routable: true` (inconsistent) now refuses where today it passes. That input does not occur on any core that reports both; refusing it is the fail-closed direction.

The header comment's "the gate is the inventory's own verdict booleans, not the length of the lists" is rewritten: `Routable` and `Discriminated` still gate, and `Collisions` gates as well because it is the one list whose presence invalidates the other reports.

### 3. The collision finding

```go
func collisionFinding(inv *platform.ContractInventory) string {
	keys := slices.Sorted(slices.Values(inv.Collisions))

	var b strings.Builder
	fmt.Fprintf(&b, "platform is not routable: %s; a platform package cannot be generated until all but one of the registry entries defining each is disabled:",
		counted(len(keys), "colliding contract", "colliding contracts"))
	for _, key := range keys {
		entries := slices.Sorted(slices.Values(inv.CollidingEntries[key]))
		fmt.Fprintf(&b, "\n  %s defined by %s", key, strings.Join(entries, ", "))
	}
	return b.String()
}
```

Example, two majors of the opm catalog sharing two keys:

```text
platform is not routable: 2 colliding contracts; a platform package cannot be generated until all but one of the registry entries defining each is disabled:
  opmodel.dev/catalogs/opm/resources/container@v1beta1 defined by opmodel.dev/catalogs/opm@v4, opmodel.dev/catalogs/opm@v5
  opmodel.dev/catalogs/opm/traits/backup@v1alpha1 defined by opmodel.dev/catalogs/opm@v4, opmodel.dev/catalogs/opm@v5
```

Entries are read from `CollidingEntries`, never derived from `DefinedBy` (which lacks the key by construction) and never counted. Both lists are sorted into copies, so the message is order-independent (`failReconcile` gates its warning event on an unchanged message) and the inventory, which the store may hold, is never mutated. A key missing from `CollidingEntries` (an inconsistent inventory) prints "defined by " with an empty list rather than being dropped; the refusal still holds. No enhancement reference goes into the message (AGENTS.md: output strings reach people without the enhancements repo).

### 4. Precedence `ContractCollisions` > `OverSubscribedContracts` > `ComparablePredicates`

One condition carries one reason. A collision comes first because it distorts the other reports (a colliding key is in none of `definedBy`, `requiredBy`, `comparable`, so the comparable finding may miss a pair and `fulfilled` can read true), and because its fix changes which catalogs are enabled, which reshapes every other report. Every finding stays in the message, collision first, as over-subscription and comparable pairs share one today, so one pass shows all of them. Over-subscription and a collision can co-occur (`providedBy` and `overSubscribed` are independent of `defined`), so the joint case is pinned by a table row.

### 5. No render-side wording

`renderFailureReason` is unchanged. B's `ContractCollisionsError` and `NotRoutableError` would fall through to `RenderFailed` with the kernel's message, which names the keys and entries. The only way a render reaches them is a recorded colliding package, which the gate forbids; a package recorded by an older operator cannot collide because no core before A's evaluates a colliding platform. Adding a render reason would be code for an unreachable path (Principle VII).

### 6. Sections

1. **Library pin to B's head, spike** (`fix(deps)`): the pseudo-version; confirm B's surface with `go doc`; confirm the registry-backed Platform specs still build and read `Collisions` empty on the generated platform, now pinned to A's core.
2. **Name the collision** (`fix(controller)`): reason constant, red rows, gate and finding, Ginkgo spec, API comment and regenerated CRD and installer, docs.
3. **Library pin to B's release** (`fix(deps)`): stop and report if B is not released; cross-cutting e2e.

Each ends green under `task dev:fmt dev:vet dev:lint dev:test` (after `task dev:manifests dev:generate` in section 2).

### Reconcile phase impact

- **Source / Render / Apply / Prune:** none. Instances on a fresh cluster whose Platform collides wait at `PlatformNotReady` with the cause on the Platform, as they do today under `BuildFailed`; on a cluster holding a last good package, renders keep consuming it.
- **Status:** a colliding Platform moves from `Ready=False` `BuildFailed` (a CUE conflict on `#contracts.definedBy`) to `Ready=False` `ContractCollisions` naming keys and entries. A platform both colliding and over-subscribed or undiscriminated reports `ContractCollisions` with every finding. An unroutable inventory with no rows keeps `OverSubscribedContracts` with a truthful message. All other outcomes are unchanged.

## Research & Decisions

### Why the operator sees a collision at all after A

**Context**: The gate only runs if the platform builds.
**Explored**: B's `DefaultSchemaModule` move (orchestration.md); the operator generates its platform module with core pinned from the library, with no operator constant (single-source-provider-count design, measured).
**Decision**: Treat the reason as reachable only after section 1's library pin, and measure the generated core pin in the section 1 spike.
**Rationale**: Before B, the operator pins core `2.0.0-alpha.12`, on which a collision is a build failure; after B it pins A's core, on which it is an inventory report.

### Gate on the list or on the boolean

**Context**: The existing gate reads only `Routable` and `Discriminated`.
**Explored**: A's fold (`routable: len(overSubscribed) == 0 && len(collisions) == 0`); A's limitation (`fulfilled` and `discriminated` can read true under a collision); B's `NotRoutableError` catch-all.
**Decision**: Refuse on a non-empty `Collisions` independently of `Routable`, print findings by list, and keep `!Routable` as the fail-closed backstop.
**Rationale**: The list is the one report whose presence makes the rest untrustworthy; the boolean remains the guard against a core term the operator does not know about.

## Risks / Trade-offs

- [B renames `Collisions`, `CollidingEntries` or changes their shape] -> code against B's reported `surface`; a rename is a mechanical edit in sections 1 and 2, reported under `deviations`.
- [B's head moves after section 1 pins it] -> the pseudo-version is only a development pin; section 3 re-pins B's release and reruns every gate.
- [A's core is not on GHCR when section 1 runs, so the registry-backed specs fail to build the generated platform] -> section 1 waits on A's release (B's start condition), and `OPM_TEST_REGISTRY_FORCE=1` turns a skip into a failure.
- [B's `Contracts()` returns an error for a colliding platform instead of an inventory] -> the platform would report `BuildFailed` naming B's error and this change's reason would be unreachable; the interface says it decodes, and section 1's `go doc` check reads the `Contracts()` doc; a contradiction is a blocker to report, not to code around.
- [Messages grow: a collision plus over-subscription prints two "platform is not routable" headers] -> accepted; each header counts its own list and names its own remedy.
- [An old operator (library alpha.35) meets a colliding platform] -> its `Roots` pin core `2.0.0-alpha.12`, where the platform fails to build (`BuildFailed`), but the closure's MVS resolves A's core as soon as an enabled catalog release requires it; the platform then builds and today's `inventoryRefusal` refuses it on `Routable: false` as `OverSubscribedContracts` with "0 over-subscribed contracts". Either way it never records or renders one. The old-kernel render hazard in orchestration.md concerns the cli and hand-pinned platforms, not the operator's generated module.

## Migration Plan

Pre-GA, no migration. Merge after B is released and pinned (section 3). Rollback is reverting the PR, which restores the library pin (and so core `2.0.0-alpha.12`, where a colliding platform is `BuildFailed` again) and the old wording together.
