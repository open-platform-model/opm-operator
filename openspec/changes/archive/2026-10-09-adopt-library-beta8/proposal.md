## Why

The operator pins library v1.0.0-beta.7. Library v1.0.0-beta.8 is released and the release cascade proposed the bump (opm-operator#272), but that PR fails `Lint` and is labelled breaking: beta.8 tidies the library's public surface (library#223) and marks two names the operator still uses as deprecated. The library wants to delete those names before v1.0.0, so the operator has to leave them now. Beta.8 also changes two behaviours the operator's conditions depend on (library#222, library#227), and the bump has to land as a reviewed change that says what each one does to the operator.

## What Changes

- `go.mod` and `go.sum` move `github.com/open-platform-model/library` from v1.0.0-beta.7 to v1.0.0-beta.8, the same two-file diff as the cascade's branch. No other pin moves.
- The operator leaves every deprecated library name. `oerrors.IdentityError` is built as a pointer and matched with a pointer target (one classifier, four test literals, one test match), and the one `catalog.Source` use becomes `module.Source`. After this the library can give `IdentityError` a pointer receiver and delete `IdentityError.As` and the `catalog.Source` alias without breaking the operator.
- A registry token endpoint's answer is classified by its status when a dependency load carries it (library#222). On beta.7 a 401 or a 429 from the token endpoint during a dependency load read as an unreachable registry. For a ModuleInstance, a ModulePackage and the Platform the outcome does not move: every typed fetch failure retries on the backoff as a non-stalled `ResolutionFailed`, before and after. For an accepted TransformerRegistration the outcome moves to what the spec already says: a refused credential (401) un-accepts the claim, where beta.7 held it as a registry outage. An unreachable registry and a 5xx answer, from the registry or from its token endpoint, still hold the claim. A 429 answer holds it too (supervisor ruling, 2026-10-09): `keepsVerdict` reads the 429 from the typed status, and since that status does not say which endpoint answered, a 429 from the registry itself now holds the claim as well, where it un-accepted before.
- The kernel now names every unset required `#config` value at `values.<field>` (library#227). A ModulePackage has no check of its own, so its message moves when a component reads the unset value: from `not fully concrete: components.<path>: incomplete value <type>` to `not fully concrete: values.<field>: incomplete value <type>`, with `(and N more errors)` when more values are unset. The reason (`ResolutionFailed`) and the stall do not move, and the new text is what `modulepackage-kernel-rendering` already requires. The ModuleInstance message does not move: the renderer's own check of `spec.values` runs first and already names every one at `#config.<field>`. The two reports now name the same fields at the same positions, and a test pins that, so the held change that removes the operator's check can rely on it.
- Tests pin each of these, and the conditions page names the token endpoint.

Not in this change: removing the values pre-check, the ownership and deletion work, any use of `*oerrors.ConfigValidationError`, RBAC, the CRDs.

SemVer class: PATCH. No behaviour moves away from what the specs state. Two things a user sees change, and both move to what a main spec already requires: a claim is un-accepted on a token endpoint refusal met during a dependency load (`registration-acceptance`, refused credentials), and a ModulePackage message names the unset value (`modulepackage-kernel-rendering`). On the beta line it ships as the next `-beta.N`, with no `!` in the PR title.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registration-acceptance`: adds that an answer from the registry's token endpoint is judged by its status, so a refusal un-accepts a claim and a 5xx or a 429 answer holds it; a 429 from the registry holds it too.
- `modulepackage-kernel-rendering`: adds that an unset required value a component reads is named as `values.<field>`, and that further unset values are counted.
- `module-instance-synthesis`: adds that the refusal of unset required values names every such value, one finding for each.

## Impact

- Affected kinds: TransformerRegistration (a token endpoint 401 met during a dependency load moves from held to refused; a 429 answer holds an accepted claim); ModulePackage (the message for an unset required value that a component reads). ModuleInstance and Platform: no behaviour change.
- Controllers: `keepsVerdict` in `internal/controller/transformerregistration_controller.go` also holds on a 429. `internal/reconcile/resolution.go` changes one type argument and one comment.
- Dependencies: one direct Go dependency moves; no new module enters `go.sum`.
- Tests: `internal/render`, `internal/reconcile`, `internal/controller`, `test/integration/reconcile`.
- Docs: `docs/site/diagnostics/operator-conditions.md`.
- Users: none have to act.
