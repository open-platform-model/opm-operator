# Tasks: registration-driven-regeneration

Four sections. Section 1 is the store re-key, first because everything else builds on the new
identity and a partial migration would leave two notions of identity in one package. design.md's
research entries are readings of code in this repo, so section 1 is a refactor, not a spike.

**Depends on `registration-activation`.** This change reads `status.active`; until that ships the
active set is always empty and every test here is vacuous.

## 1. The store holds one package per identity

- [x] 1.1 Introduce the package identity — the `Platform` CR generation plus the sorted `catalog@version` list of active claims — as a value type with a stable string form, and unit-test that the same inputs yield the same identity and any change yields a different one. Verify: ordering of the input claim list does not change the identity.
- [x] 1.2 Re-key `internal/platform.Store`: `generated`, the current-package lookup and `leases` move from `generation int64` to the identity. Move all four callers in the same commit — `platform_controller.go` (`SetGenerated`), both renderers (`Lease`) and `transformerregistration_controller.go` (`Lease`) — because `Generated.Generation` and `Leased() []int64` are read by callers, so the key is not encapsulated (design.md § The store's key is load-bearing in four places). Verify: lease semantics are unchanged — a superseded package stays readable while held and is reclaimed when released; the render-path tests pass untouched.
- [x] 1.3 Follow the Platform reconciler's prune to the new key, so a superseded package's directory survives while leased. Verify: a test regenerates while a lease is open and asserts the old directory is not removed.
- [x] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `refactor(platform): hold one generated package per identity`.

## 2. Regeneration reads the active-claim set

- [x] 2.1 Add a `TransformerRegistration` watch to the Platform reconciler, waking regeneration when any claim changes. Verify: activating a claim regenerates without the `Platform` CR being edited.
- [x] 2.2 Compute the generated package from the tuple — the CR spec plus the accepted-and-active claims — reading current state rather than the waking event's content, and add an import and a `#registry` entry per active claim. Verify: an active claim's catalog is a registry entry at the claim's version; a stale or duplicated event yields the package the current state implies; with no active claims the package is byte-identical to what the spec alone produces today.
- [x] 2.3 Cover the burst case: several claims activating together converge in one or a few regenerations, not one per claim. Verify: the test asserts a bound on regeneration count, and that the final package reflects every claim.
- [x] 2.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(controller): regenerate the platform from the active-claim set`.

## 3. Platform status carries the identity and the union

- [ ] 3.1 Add the package identity and the resolved registry union to `PlatformStatus`, each union entry indicating whether it came from an authored subscription or an active claim. Then `task dev:manifests dev:generate`. Verify: the generated CRD carries both; `zz_generated.deepcopy.go` gains the new types.
- [ ] 3.2 Write both on every generation, and document in the field's doc comment that the identity — not `status.active` on a claim — is the authoritative answer to what a render is building against, because the two reconcilers are eventually consistent (design.md § the claim reconciler stays the judge). Verify: the union follows the active set in both directions; the doc comment states the consistency caveat.
- [ ] 3.3 Report the consumed identity on every render, so a render is attributable to an exact registry state. Verify: a render against a superseded package reports the superseded identity, not the current one.
- [ ] 3.4 `task dev:manifests dev:generate`, then `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(api): surface the effective registry and its package identity`.

## 4. The acceptance-regeneration loop

- [ ] 4.1 Test the loop design.md names: two claims activating in the same burst where each provides a contract that would refuse the other. Assert arbitration is deterministic under D12's earliest-`creationTimestamp` holder and does not depend on event order or on which reconciler ran first. Verify: the test runs the burst repeatedly and reaches the same outcome each time.
- [ ] 4.2 Confirm the check the loop closes on cannot refuse a claim against itself: a claim's own contracts must not count as already-provided when it is the one being judged. Verify: a single active claim re-judged after regeneration stays accepted.
- [ ] 4.3 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(controller): pin the acceptance-regeneration loop's arbitration`.
