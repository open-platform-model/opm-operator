## Why

The operator pins library v1.0.0-beta.6. The release cascade proposed the bump to v1.0.0-beta.7 (opm-operator#264, `go.mod` and `go.sum` only), but its green run predates four merges on `main`, so the combination is untested. Library beta.7 carries three breaking changes. The operator's kernel work (ownership, the deletion protocol, the Flux-order test, dropping the values pre-check) starts on beta.7, so the bump has to land as a reviewed change that says what each break does to the operator.

## What Changes

- `go.mod` and `go.sum` move `github.com/open-platform-model/library` from v1.0.0-beta.6 to v1.0.0-beta.7, the same two-file diff as the cascade's branch. No other pin moves.
- **BREAKING**: a ModulePackage whose values leave a required `#config` value unset, where no component reads that value, is now refused when the package loads. The object reports `Ready=False` and `Stalled=True` with reason `ResolutionFailed` and the message `loading package: Kernel.AcquireInstanceFromDir: instance "<name>": not fully concrete: values.<field>: incomplete value <type>`. On beta.6 such a package rendered and applied. The kernel forces this; the operator has no check of its own on that path to keep the old behaviour with.
- A ModuleInstance in the same state keeps its condition and its message, unchanged: the renderer's own check of `spec.values` against `#config` runs before synthesis and stays in this change. Tests pin both paths.
- No other breaking change of beta.7 reaches the operator today (design.md gives the evidence for each): the kind weights, the adopt rule and the lifecycle package, and the typed resolution errors.
- A comment that said synthesis does not refuse an unset required value is corrected, and the conditions page names the new ModulePackage refusal.

Not in this change: adopting `opm/k8s/ownership` or `opm/k8s/lifecycle`, removing the values pre-check, RBAC, the CRDs.

SemVer class: MAJOR after GA (a package that rendered is now refused). On the beta line it ships as the next `-beta.N`, with `!` in the PR title.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `modulepackage-kernel-rendering`: adds the refusal of a package whose values leave a required `#config` value unset.
- `module-instance-synthesis`: adds the requirement that a ModuleInstance in that state is refused with the `spec.values` wording and reason `RenderFailed`.

## Impact

- Affected kinds: ModulePackage (new refusal). ModuleInstance, Platform and TransformerRegistration: no behaviour change.
- Controllers: none edited. `internal/render/kernel_module_renderer.go` gains an unexported split of `synthesize` so a test reaches the values check without a registry fetch of the module.
- Dependencies: one direct Go dependency moves; no new module enters `go.sum`.
- Tests: `internal/render/required_values_test.go`, two rows in `internal/reconcile/resolution_test.go`.
- Docs: `docs/site/diagnostics/operator-conditions.md`.
- Users: a ModulePackage author sets the value, gives the `#config` field a default, or marks it optional with `?`.
