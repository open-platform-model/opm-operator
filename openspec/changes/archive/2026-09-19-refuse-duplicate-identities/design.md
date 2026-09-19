# Design: refuse-duplicate-identities

## Context

See `proposal.md` § Why. The decision is `enhancements/0015` D15 (one registration per module; a second refused loudly, naming both carrying components, before apply), with D12 (the registration's instance-derived name) as the reason two registrations collide. The library's `render-duplicate-identities` shipped the detector; this change is the operator's refusal.

Current state, read 2026-09-19 (opm-operator main at `126e8d3`, library `v1.0.0-alpha.32`):

- `internal/render/kernel_module_renderer.go` `resultFromRender(out *kernel.RenderResult, identity, contracts)` is the one adapter both renderers call after `Kernel.Render`: it wraps each `Compiled` into `core.Resource`, builds inventory entries through `ToUnstructured`, words the warnings and carries the diagnostics rows. It is a pure function of the render output.
- `internal/reconcile/resolution.go` `renderFailureReason(err, isResolutionMsg)` maps a render error to a Ready reason by typed cause in precedence order (skew, resolution class, everything else `RenderFailed`); both loops call it (`moduleinstance.go` `classifyRenderError`, `modulepackage.go`). Every classified refusal is `MarkStalled` plus a Warning event and a stalled-interval requeue.
- `internal/status/conditions.go` gives every refusal a reason of its own, documented with the state it reports and why it is distinct.
- Library `alpha.33` `opm/helper/objectset`: `Duplicates(compiled []*kernel.Compiled) []Duplicate` (identity plus producers in render order, values without kind or name skipped) and `*DuplicateIdentitiesError{Duplicates}` whose `Error()` names each identity once and every producer as component and transformer.
- Tests: `internal/render/warnings_test.go` builds `kernel.RenderDiagnostics` by hand and asserts wording; `internal/reconcile/resolution_test.go` drives `renderFailureReason` with typed errors. The registry-backed specs render fixture modules whose objects have distinct identities; the fixtures pin catalog `4.0.1`, which predates the registration contract (`4.3.0`).
- `docs/RENDERING.md` has a Ready-reason table (`PlatformNotReady`, `ResolutionFailed`, `SkewRefused`, `RenderFailed`).

## Goals / Non-Goals

**Goals**

- No render with two objects of one identity reaches apply, on either reconcile path.
- The refusal names both producers, in the library's wording, under a reason the module author can act on.
- The two classifiers stay one function.

**Non-Goals**

- Deduplication, arbitration or last-wins by design.
- An end-to-end fixture with two registrations (see Research & Decisions).
- Any change to the claim reconciler: a duplicate never becomes a claim, because it never reaches apply.

## Decisions

### The check sits first in the adapter

```go
func resultFromRender(out *kernel.RenderResult, identity platformstore.PackageIdentity, contracts []string) (*RenderResult, error) {
	if dups := objectset.Duplicates(out.Compiled); len(dups) > 0 {
		return nil, &objectset.DuplicateIdentitiesError{Duplicates: dups}
	}
	// ... resources, inventory entries, warnings as today ...
}
```

Before any `core.Resource` is built, so a refused render produces no digest, no inventory entry and no partial result either loop could act on. The error is returned bare (not wrapped) so `errors.AsType` finds it in both classifiers and the message reaches status verbatim.

### Classification

```go
// resolution.go
func isDuplicateIdentities(err error) bool {
	_, ok := errors.AsType[*objectset.DuplicateIdentitiesError](err)
	return ok
}

func renderFailureReason(err error, isResolutionMsg func(error) bool) string {
	switch {
	case isSkewRefusal(err):          return status.SkewRefusedReason
	case isDuplicateIdentities(err):  return status.DuplicateIdentitiesReason
	case isTypedResolutionError(err), isResolutionMsg(err): return status.ResolutionFailedReason
	default:                          return status.RenderFailedReason
	}
}
```

Placed after skew (a skew refusal happens before evaluation, so nothing was rendered) and before the resolution class (a duplicate is a verdict on the render's output, and it must not be mistaken for a platform problem). Both loops already turn the returned reason into `Ready=False`, `Stalled=True`, a transition-gated Warning event and the stalled requeue; nothing else changes in either.

### The reason

```go
// DuplicateIdentitiesReason: Ready=False, two or more rendered objects share
// one Kubernetes apply identity (apiVersion, kind, namespace, name), so the
// render is refused before apply and the message names each identity and
// every producing component and transformer (0015:D15). Distinct
// from RenderFailed because nothing failed to evaluate and the platform is
// not at fault: the module author removes or renames a component.
DuplicateIdentitiesReason = "DuplicateIdentities"
```

### Reconcile phase impact

| Phase | Change |
| --- | --- |
| Source | none |
| Render | adapter refuses before building resources; new reason on classification |
| Apply | never reached for a refused render |
| Prune | none (a refused render records no inventory, so the prior inventory stands, as for every other refused render) |
| Status | one new `Ready=False` reason; conditions, events and requeue as every other refusal |

### Files touched

| File | Change |
| --- | --- |
| `go.mod`, `go.sum` | library `v1.0.0-alpha.33` |
| `internal/status/conditions.go` | `DuplicateIdentitiesReason` |
| `internal/render/kernel_module_renderer.go`, new `kernel_module_renderer_test.go` (or beside `warnings_test.go`) | the check; adapter tests |
| `internal/reconcile/resolution.go`, `resolution_test.go` | the typed arm; classifier test |
| `docs/RENDERING.md` | the reason row and one sentence in the render section |

## Research & Decisions

### Unit tests over the adapter, not a two-registration fixture

**Context**: D15's motivating case is two `transformer-registration` components in one module, and an envtest rendering such a module would be the most literal proof.
**Explored**: (a) a fixture provider module with two registration components under `test/fixtures/modules`, bumping the fixture catalog pin to a build carrying the registration contract; (b) hand-built `kernel.RenderResult` values driving `resultFromRender`, plus a classifier test.
**Decision**: (b).
**Rationale**: the refusal is a pure function of the kernel's compiled output; the identity fields are read off values the test builds with `cuecontext`, and the two-registration case is expressible exactly (two `Compiled` values, kind `TransformerRegistration`, one name, components `registration` and `registration-copy`). (a) moves every fixture's catalog pin for one test, through a publish pipeline shared byte-for-byte with the cli, and it would exercise the same six lines. The registry-backed specs keep proving the healthy path.

### A reason of its own, not `RenderFailed`

**Context**: `RenderFailed` is the catch-all for evaluation errors and over-subscription.
**Explored**: folding the duplicate into `RenderFailed`; a distinct reason.
**Decision**: distinct.
**Rationale**: `conditions.go`'s stated rule is that every refusal gets a reason of its own because a claimant acts on the reason. A `RenderFailed` reader looks at transformers and the platform; a duplicate is fixed in the module's components. D15's "refused loudly, naming both carrying components" is met by the message either way; the reason is what makes it findable in a fleet.

### The error stays the library's

**Context**: the operator could wrap the helper's error in its own type or re-word it.
**Explored**: wrapping in a `render`-package error; returning the library error bare.
**Decision**: bare.
**Rationale**: the library exists so the cli and the operator refuse with one wording; the classifier reaches the type through `errors.AsType` either way, and a wrapper would exist only to be unwrapped.

## Risks / Trade-offs

- [A module in the fleet renders two objects with one name today and was silently losing one] → after upgrade it reports `DuplicateIdentities` naming both components and stops applying; the last applied inventory stands until the module is fixed, which is the same posture as every other render refusal.
- [The `objectset` import puts a helper-tier package into the operator] → intended: the helper tier is opt-in convenience for Kubernetes-applying frontends, which the operator is.
- [Library bump moves nothing else] → `alpha.32` to `alpha.33` is the helper package alone (one commit); no kernel signature changed.

## Migration Plan

One PR, two sections, squash title `feat(render): refuse a render whose objects share one apply identity`. release-please cuts the next operator pre-release. Rollback is a revert; no stored state changes.

## Open Questions

None.
