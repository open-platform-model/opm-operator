## Context

See proposal.md for the motivation. The operator uses these library packages: `opm/errors`, `opm/kernel`, `opm/module`, `opm/platform`, `opm/catalog`, `opm/schema`, `opm/helper/platformmodule`, `opm/k8s/health`, `opm/k8s/inventory`, `opm/k8s/labels` and `opm/k8s/object`. It applies with Flux's `ResourceManager.ApplyAllStaged` (`internal/apply/apply.go`) and prunes and deletes with one `client.Delete` per inventory entry (`internal/apply/prune.go`).

## Goals / Non-Goals

**Goals:**

- The same pin set as the cascade's branch, on today's `main`.
- A verdict with evidence for each breaking change of library v1.0.0-beta.7.
- Tests that fail on the next bump if a verdict stops holding.

**Non-Goals:**

- Using `opm/k8s/ownership` or `opm/k8s/lifecycle`.
- Removing the values pre-check in `KernelModuleRenderer`.
- Any change to RBAC or a CRD.

## Research & Decisions

### How the bump is made

**Context**: The brief asks for the cascade's pin set without building on its commits.
**Explored**: `task -x deps:cascade` runs `go get <library>@<version>` and `go mod tidy` for the library (`.tasks/cascade/cascade.sh`), then moves the catalog, core, fixture and cli pins the resolver reports. The cascade's branch moved only `go.mod` and `go.sum`.
**Decision**: Run `go get github.com/open-platform-model/library@v1.0.0-beta.7` and `go mod tidy` by hand.
**Rationale**: The same two commands as the task's library step, without the resolver moving a pin the cascade's branch does not have. The changed lines are compared with `git diff origin/main...origin/deps/cascade` and are identical. The Go checksum database verified the module; no new module enters `go.sum`.

### Required values (library#217)

**Context**: `SynthesizeInstance` and `AcquireInstanceFromDir` now refuse values that leave a required `#config` value unset, even when no component reads it (`opm/kernel/process.go`, `requiredConfigSet`).
**Explored**: A probe with the hello fixture plus `#config: note: string`, run on both library versions.

| Path | beta.6 | beta.7 |
| --- | --- | --- |
| ModuleInstance (`synthesize`, pre-check then `SynthesizeInstance`) | `validating values against the module's #config: #config.note: incomplete value string (<file>:3:16)` | the same text |
| `SynthesizeInstance` alone, without the pre-check | no error | `Kernel.SynthesizeInstance: instance "needy": not fully concrete: values.note: incomplete value string` |
| ModulePackage (`AcquireInstanceFromDir`) | loads; the render goes on | `loading package: Kernel.AcquireInstanceFromDir: instance "needy": not fully concrete: values.note: incomplete value string` |

The kernel's check also refuses a value no component reads whose default disagrees with the `#config` default: a package with `values: tier: string | *"b"` against `#config: tier: string | *"a"` fails on beta.7 with `not fully concrete: values.tier: incomplete value string | "a" | "b"`. Only the ModulePackage path can reach this state: `spec.values` is JSON and carries no default.

**Decision**: Keep the pre-check as it is. Accept the new ModulePackage refusal as the breaking change of this bump and pin both paths with tests.
**Rationale**: On the ModuleInstance path the pre-check runs first, so the condition (`RenderFailed`, stalled) and the message do not move. On the ModulePackage path there is no operator check and no way to keep accepting the package short of not taking the release. The refusal is marked `ErrAcquire` like every package load failure, so its reason is `ResolutionFailed` and it stalls (it holds no `*FetchError`).
**Alternative considered**: Classify the ModulePackage refusal as `RenderFailed` to match the ModuleInstance reason. Rejected here: it needs message matching or a typed kernel error that beta.7 does not give, and a non-concrete package already reports `ResolutionFailed` today.

### Kind weights (library#214)

**Context**: Thirteen `opm/k8s/object` weight constants changed and three were added.
**Explored**: In the library at v1.0.0-beta.7 the weight table is read only by `object.Sort` (`opm/k8s/object/sort.go`), `object.Stages` (`stages.go`) and the lifecycle deletion plan (`opm/k8s/lifecycle/plan.go`). The operator calls none of them: the `object` symbols it uses are `Duplicate`, `Duplicates`, `DuplicateIdentitiesError`, `Export`, `ExportDecode`, `Exported`, `ExportError`, `Identity`, `ObjMetadata`, `Producer`, `Resource`, `Resources` and `UnstructuredToObjMetadata` (grep over `internal`, `cmd`, `test`, `api`). Apply order is Flux's own staged sort. Prune and delete walk `status.inventory` or the stale set in inventory order (`inventory.StaleSet` returns "previous order", and `opm/k8s/inventory` did not change between the two tags). Status output sorts nothing by kind.
**Decision**: No edit and no order test.
**Rationale**: No operator path sorts by the library's weights, so no order changed. A test would pin Flux's order, which the operator's planned Flux-order test owns.

### The adopt rule and the lifecycle package (library#220, library#209)

**Context**: `ownership.CanApply` and `CanDelete` changed verdicts; `opm/k8s/lifecycle` is new.
**Explored**: The operator imports neither package (list under Context).
**Decision**: No edit.

### Typed resolution errors (library#218)

**Context**: `oerrors.Classify` now wraps an author-defect resolution failure (an unprovided or ambiguous import, a dependency module file that does not parse) in `*oerrors.ResolutionError`.
**Explored**: Its `Error()` is the cause's message and it unwraps to the cause. The fetch forms are matched first and did not change, so nothing that was a `*FetchError` becomes a `*ResolutionError`. The operator's classifiers read `*FetchError`, `IdentityError`, `*UnresolvedDemandsError`, `*UnmatchedComponentsError`, three sentinels and `render.ErrAcquire` (`internal/reconcile/resolution.go`); none reads the new type.
**Decision**: No edit. Pin the outcome: a `*ResolutionError` under acquisition stalls as `ResolutionFailed`, under synthesis as `RenderFailed`, and a package with an unprovided import carries the type and no `*FetchError`.
**Rationale**: Condition, reason, retry behaviour and message are what they were. Using the type to give such failures a reason of their own is a follow-up, not forced by the bump.

### Other beta.7 entries

The platform's core floor and contract inventory are decoded at construction (library#208), the acquire refuses a synthetic root that exists on disk (library#213), and the render module is staged in memory with one registry client per kernel (library#212). `task dev:test` passes on beta.7 with no test edited, which covers the operator's use of all three. `platform.Platform` now holds a `sync.Once`; `go vet` reports no copy.

## Reconcile phase impact

- Source: none.
- Render: a ModulePackage with an unset required `#config` value now fails at the package load. ModuleInstance unchanged.
- Apply: none (Flux's staged order).
- Prune: none (inventory order).
- Status: the new failure reports `ResolutionFailed`, stalled, with the kernel's message.

```go
// internal/render/kernel_module_renderer.go: the only non-test Go edit.
func (r *KernelModuleRenderer) synthesize(ctx, name, namespace, modulePath, moduleVersion, values) (*module.Instance, error) {
	mod, err := moduleacquire.Acquire(ctx, r.Kernel, modulePath, moduleVersion, r.Registry)
	if err != nil {
		return nil, acquireFailed("acquiring module", err)
	}
	return r.synthesizeFrom(ctx, mod, name, namespace, values) // pre-check, then SynthesizeInstance
}
```

## Risks / Trade-offs

- [A ModulePackage that reconciled on the last operator release stalls after the upgrade] → The PR title carries `!`, the PR body carries the migration, and the conditions page names the cause.
- [The kernel's ModulePackage message names `values.<field>` with no file position] → Accepted; improving it is library work.
- [No e2e run in this change] → The CI e2e job on the PR is the proof. No e2e or integration assertion names a weight, an order or a required-value message; the four modulepackage fixtures render on beta.7 in `test/integration/reconcile`.

## Migration Plan

A ModulePackage author whose package is refused for an unset value sets it, gives the `#config` field a default, or marks it optional with `?`. For a default that disagrees with the `#config` default, the author makes the value concrete or makes the two defaults agree. Rollback is a revert of the bump commit.
