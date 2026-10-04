## Why

The integration spec "Apply When applying a CRD and an instance of it together should apply the CRD before the custom resource regardless of input order" (`test/integration/apply/apply_test.go:176`) failed once on `main`: Tests run 37176383926, attempt 1, 2026-10-04 04:16 UTC, on commit 2f2d5ded. The failure was at `apply_test.go:225`:

```
failed to apply resources: Widget/default/apply-order-widget dry-run failed:
no matches for kind "Widget" in version "test.example.com/v1"
  err: *errors.DryRunErr{underlyingErr: *meta.NoKindMatchError{Group: "test.example.com", Kind: "Widget", SearchedVersions: ["v1"]}}
```

Attempt 2 of the same run passed. The race is real in the product code, not only in the test:

- `apply.Apply` (`internal/apply/apply.go:42`) calls Flux `ApplyAllStaged` (`fluxcd/pkg/ssa@v0.77.0`, `manager_apply.go:347-420`). That function applies the CRD in its definitions stage, waits for it through `WaitForSet` (`:385`), and only then applies the custom resource (`:413`).
- `WaitForSet` uses kstatus, which reports a CRD as Current once its `Established` condition is True (`fluxcd/cli-utils`, `kstatus/status/core.go:601-627`).
- `Established` does not mean that discovery already serves the new kind. The custom resource's `Get` and server-side dry run (`manager_apply.go:236-283`) resolve the kind through the client's RESTMapper. The mapper re-reads discovery on a miss (controller-runtime v0.24.1, `pkg/client/apiutil/restmapper.go:122-132`, `:311-345`), but discovery can still lack the kind for a few milliseconds. The whole failing spec took 30 ms.
- Production reconcile has the same path: `internal/reconcile/moduleinstance.go:423` and `internal/reconcile/modulepackage.go:496`. On a first apply of a module that ships a CRD and an instance of it, the reconcile can hit the same miss. It then emits a Warning `ApplyFailed` event, sets `Ready=False` with reason `ApplyFailed`, increments `failureCounters.reconcile`, and requeues on the backoff (`moduleinstance.go:424-431`, `modulepackage.go:497-501`). The next attempt succeeds. The object heals, but a user sees a spurious failure, and the failure counter moves.

The failure blocks nothing by itself, but a flaky required check matters more once the cascade lands. Under the cascade (workspace `RELEASING.md`, "The cascade" and "Gates"), bot-opened `deps/cascade` PRs and release PRs in opm-operator wait on `Tests`. A random red there costs a human a re-run, and the cascade relies on CI that tells the truth.

## What Changes

- **`apply.Apply` waits for discovery of a CRD it is applying.** When `ApplyAllStaged` fails only because the API server does not serve a kind yet (`meta.IsNoMatchError`), and a `CustomResourceDefinition` in the same resource set defines that kind, `Apply` retries the whole staged apply. The retry is bounded: every 500 ms, no new attempt once 10 s have passed since the first retryable failure, and never past the caller's context. Each attempt runs under the caller's context unchanged, so an apply that runs long today is not cut short. SSA apply is idempotent, so re-running the set is safe.
- **No retry for any other no-match.** A custom resource whose CRD is not in the set fails at once, as today. So does any error that is not a no-match. A genuine missing CRD in some other module is never delayed.
- **Counts stay truthful across a retry.** `ApplyResult` reports each object by the first attempt that created or configured it. A CRD that the first attempt created counts as `Created` even though the retry sees it `Unchanged`. The `Applied N resources (...)` event and log stay correct.
- **Tests.** A lagging RESTMapper in the apply integration suite simulates discovery lag deterministically. It reproduces the CI error shape before the fix and proves the retry after it; it tests the retry logic, not the race. A stress spec that applies 20 new CRDs and their instances in one call tries for the real race before and after the fix. Further specs cover the no-retry and context-end cases. Unit tests in `internal/apply` cover the predicate, the ledger and the loop, including its bound and that no attempt gets a deadline the caller did not set.
- **Housekeeping on the touched code.** The `apply.go` doc comment, the `Staged apply ordering` requirement and a scenario of `SSA apply with opm-controller field manager` cite `docs/design/flux-ssa-staging.md`, which does not exist. The first two state the stage model inline instead; the scenario drops the citation. The test's spec reference comment (`apply_test.go:173-174`) points at the main spec, not the long-archived change `08-ssa-apply`.

## Classification

**PATCH**. Before GA it ships as the next `1.0.0-beta.N` under a `fix(apply)` PR title. A `fix` releases the operator (`AGENTS.md`, "Commit type decides the release"). Once the cascade is wired, that release dispatches to the cli (workspace `RELEASING.md`, "Notify after publish": opm-operator → cli), which is the intended effect for a shipped behaviour fix.

No API type, field, CRD schema, reason constant or flag changes.

Complexity (Principle VII): one bounded retry loop and one predicate. The alternative of a test-only `Eventually` would hide a production gap. A RESTMapper reset would duplicate what the mapper already does on a miss and would not close the window.

## Depends on / gates

- **Depends on: none.** This change touches only `internal/apply` and `test/integration/apply`. It does not touch any workflow, `.tasks/cascade/` or `.opm-cli-version`.
- **No order relative to the Phase 3 wiring.** It is independent of `.github` `add-release-cascade-workflows` and of opm-operator `join-release-cascade` (B4 in the Phase 3 wiring contract, §1). Those must merge in their own order (A first, then the joins), but this fix may merge before, between or after them. If it merges after B4 and the receiver is live, the release it cuts will notify the cli. If it merges before, the cli picks up the release by its manual pin flow.
- **Gates:** the repo validation gates (`openspec/config.yaml`, "Validation Gates"): `task dev:fmt dev:vet dev:lint dev:test`. No API types change, so `task dev:manifests dev:generate` and `task docs:bundle:check` are not needed.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ssa-apply`:
  - A new requirement: a custom resource whose CRD is in the same apply set is retried while discovery catches up, within a bound, and nothing else is retried.
  - A new requirement: apply result counts across a discovery retry.
  - `Staged apply ordering` loses its dead design-doc link and states the stage model inline. Both of its scenarios are kept.
  - `SSA apply with opm-controller field manager` loses the same dead link from one scenario. All three scenarios are kept.

## Impact

- `internal/apply/apply.go`: the retry around `rm.ApplyAllStaged`, the predicate and the result merge.
- New `internal/apply/apply_test.go`: unit tests for the predicate, the result merge and the retry loop.
- `test/integration/apply/apply_test.go`, `test/integration/apply/suite_test.go`: the lagging mapper and the new specs.
- `openspec/specs/ssa-apply/spec.md`:
  - The delta applies at archive.
  - Before that, task 1.0 gives the file the `## Purpose` and `## Requirements` sections it lacks. Today `openspec validate ssa-apply --type spec --strict` fails on it, and archive would refuse the delta. This is one of the 17 opm-operator main specs that currently fail `--strict`.
- Downstream: none in code. The cli has its own apply path (`internal/kubernetes`), and its archived change `order-instance-apply-by-weight` handles CRD ordering itself. The cli resolves a custom resource's endpoint from its kind without client-side discovery, so it does not share this race (design.md, Risks).
- No enhancement decision backs this change, so there is no `enhancement.yaml`.
