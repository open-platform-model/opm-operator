# Design: registration-driven-regeneration

## Context

See `proposal.md`. Current state, read 2026-09-15 at alpha.19:

- `internal/platform/store.go`: `Store` holds `generated *Generated`, `generation int64` and `leases map[int64]int`. Its doc says "at most one generated platform, keyed on the Platform CR's `.metadata.generation`". `Generated` carries `{Generation, Dir, Platform *platform.Platform, Skew}`.
- Four callers: `platform_controller.go:219` (`SetGenerated`), `internal/render/kernel_module_renderer.go:77` and `kernel_package_renderer.go:71` (`Lease`), and `transformerregistration_controller.go:128` (`Lease`).
- The Platform reconciler watches only `Platform`, under `GenerationChangedPredicate`.
- The claim reconciler owns acceptance and activation and writes its verdict on the claim.

## Goals / Non-Goals

**Goals**

- An active claim's catalog reaches the render, which is the whole purpose of the second transformer path.
- A claim change produces a new package even when the CR's generation is untouched.
- A render in flight is never disturbed by a regeneration.

**Non-Goals**

- Judging claims. Acceptance and activation are shipped and untouched.
- D14's readiness exclusion, which remains unreachable.
- Removal — `registration-removal-guard`.

## Decisions

### The claim reconciler stays the judge; the Platform reconciler stays the single writer of the effective set

D13's text says the woken reconcile "runs the full acceptance battery in place", which reads as the Platform reconciler judging claims. That is not what shipped, and it should not be undone.

`registration-acceptance`'s design records why the verdict lives on the claim: folding acceptance into the Platform reconciler makes one claim's failure a platform-level failure, which is the misattribution D8 exists to prevent. What D3 actually requires is *"exactly one writer to the effective set, and one place to reject"* — and both survive:

```
  +------------------+         +---------------------+
  | claim reconciler |  writes | TransformerRegistr. |
  |  judges, gates   |-------->|  .status.accepted   |
  +------------------+         |  .status.active     |
                               +----------+----------+
                                          | reads
                                          v
  +---------------------+      +---------------------+
  | Platform reconciler |----->| Platform            |
  |  computes the tuple |writes|  .status.registry   |
  +---------------------+      +---------------------+
```

One place rejects a claim (the claim reconciler). One writer owns the effective set (the Platform reconciler). D13's property holds; only the object the battery runs on differs from its prose.

**Consequence worth stating: the two reconcilers are eventually consistent.** D13 derives atomicity from "the existing single-writer", assuming one reconciler. With two, a claim can be active for a moment before regeneration has folded it in. That is not a correctness problem — generation is level-computed, so the next regeneration catches up — but it means an observer can see `status.active` true and the effective set not yet containing it. Status should not be read as a promise that the package already reflects it; the package identity is.

### The store is keyed on identity, and that is the change's real cost

D17 is why this is not a small change. Today:

```go
type Store struct {
	generated  *Generated
	generation int64
	leases     map[int64]int
}
```

The unit becomes one generated package per D13 identity: the CR generation plus the sorted `catalog@version` list of active claims. A claim activating with the generation unchanged must yield a **new** package under a **new** identity, or the render keeps consuming a platform that does not contain the provider just accepted — which is precisely the bug D17 exists to prevent.

That turns `leases map[int64]int` into a map keyed on identity, and makes "the current package" a lookup rather than a single slot. Prune follows: a superseded package stays on disk while leased, exactly as a superseded generation does today.

**Alternative considered — keep the generation key and bump the CR's generation on every claim change.** Rejected twice over: the operator does not own the Platform CR's spec, so it cannot bump its generation, and a GitOps reconciler fighting the operator over that field is the shape D3 rejected for the webhook alternative.

### The identity is computed, never stored as intent

The identity is derived from the two inputs at generation time and stamped on the result. It is never read back as an input, and nothing reconstructs the active set from it. This keeps generation a pure function: the same tuple yields the same identity, and an identity mismatch is the signal that the tuple moved.

## Research & Decisions

### The store's key is load-bearing in four places

**Context**: how far D17's re-key reaches.
**Explored**: `internal/platform/store.go` and every caller of `SetGenerated`, `Lease`, `Generated` and `Leased`.
**Decision**: change the key in `Store` and move all four callers with it, in one section.
**Rationale**: the key is not encapsulated — `Generated.Generation` is a field callers read, and `Leased() []int64` returns generations. A partial migration would leave two notions of identity in the same package, so the re-key is atomic or it is not done.

### Regeneration and acceptance form a loop that must not oscillate

**Context**: D2's arm (in `registration-activation`) refuses a claim against contracts an enabled subscription provides, read from the built platform. This change makes the built platform depend on active claims.
**Explored**: the claim reconciler's `Store.Lease()` at `transformerregistration_controller.go:128`, and D13's level-computed rule.
**Decision**: no special machinery; rely on level-computation and record the hazard.
**Rationale**: the loop is real — claims feed the platform, the platform feeds the already-provided check — but it converges because both sides compute from current state rather than from deltas, and a claim that is refused never becomes active, so it cannot contribute a contract that would refuse itself. The case to watch in testing is two claims activating in the same burst where each would refuse the other; D12's stable holder rule (earliest `creationTimestamp`) is what makes that deterministic rather than order-dependent.

## Risks / Trade-offs

- [The store re-key touches the render path, the hottest code in the operator] -> it is one section with its callers, and the existing lease semantics are preserved exactly; only the key type changes. The render-path tests are the gate.
- [An observer can see an active claim the package does not yet contain] -> recorded above; the package identity in `Platform.status` is the authoritative answer to "what is the render building against", and status should say so.
- [Two claims activating together could each refuse the other] -> D12's earliest-`creationTimestamp` holder makes arbitration deterministic; the risk is a test gap rather than a design gap, and the research entry names the case.
- [A cluster with many active claims regenerates often] -> coalescing is inherent to level-computation, and the spec requires the burst case to converge well below one regeneration per claim.

## Open Questions

None.
