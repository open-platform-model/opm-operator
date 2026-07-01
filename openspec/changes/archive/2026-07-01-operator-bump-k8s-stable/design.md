## Context

`opm-operator` currently pins `k8s.io/*` at v0.35.2 and `sigs.k8s.io/controller-runtime` at v0.23.3. The CLI (a sibling repo in the same workspace) is already on `k8s.io/*` v0.36.0. Enhancement [0006](../../../../enhancements/0006/) recorded this skew as "Problem 3" (`research/findings.md`) and scoped closing it as slice A1 — independent prep work with no dependents, motivated by keeping the two repos on one k8s line as their dependency surface grows, rather than by any current build failure (D13/D31 mean the CLI doesn't import `opm-operator` or `controller-runtime` at all today, so there is no live MVS conflict this fixes right now).

Investigated during the `openspec-explore` session that preceded this change:

- The module proxy confirms `sigs.k8s.io/controller-runtime@v0.24.1` pins `k8s.io/* v0.36.0` directly in its own `go.mod` — an exact match to the CLI's current line, not a guess.
- `controller-runtime@v0.24.1` requires `go 1.26.0`. `opm-operator`'s current `go` directive is `1.26.2`, already above that floor — no directive bump needed here, correcting the plan's original wording in `enhancements/0006/planned-changes.md`, which called for "aligning the go directive" as part of this slice.
- `controller-runtime` v0.24.0's release notes list one breaking change beyond the k8s.io bump itself: removal of the deprecated webhook custom-path builder function. `opm-operator` ships no webhooks (`grep` found no `webhook.CustomDefaulter`, no `ctrl.NewWebhookManagedBy`, no `*webhook*` files) — this removal does not touch this repo.
- v0.24.1 itself is a bug-fix release over v0.24.0 (one cherry-picked SSA typed-error regression fix) — it is the correct target, not v0.24.0.
- Neither the Kubernetes 1.36 release notes nor the client-go v0.36.0 release notes gave a complete, groppable list of client-library-level deprecations/removals. That residual uncertainty is not resolvable by more research; it's resolved empirically by the validation gates below.

**Widened during implementation (this is the bulk of what changed in this revision of `design.md`):** running `go mod tidy` after the `k8s.io/*`/`controller-runtime` bump failed:

```
opm-operator/internal/apply imports
  fluxcd/cli-utils/pkg/kstatus/polling imports
  k8s.io/kubectl/pkg/scheme imports
  k8s.io/api/scheduling/v1alpha1: not found in k8s.io/api@v0.36.2

opm-operator/internal/apply imports
  fluxcd/pkg/ssa imports
  fluxcd/pkg/ssa/normalize imports
  k8s.io/api/autoscaling/v2beta2: not found in k8s.io/api@v0.36.2
```

`k8s.io/api` v0.36.x deletes those two API-group packages outright. `opm-operator`'s currently-pinned Flux libraries (`cli-utils` v0.37.2-flux.1, `pkg/ssa` v0.69.0) still reference them internally. This is a one-directional coupling — there is no `k8s.io/api` version that keeps those deleted packages available, so **the Flux bump is not optional**, and there is no way to sequence it separately from the k8s.io bump (you cannot land "new k8s.io/api, old Flux" as an intermediate state). See D4 for the version-selection approach and D5/D6 for why the two Flux-side breaking changes discovered during that research don't apply here.

## Goals / Non-Goals

**Goals:**

- Land `opm-operator` on the same `k8s.io/*` line (v0.36.0), a compatible `controller-runtime` (v0.24.1), and a compatible, coordinated set of Flux packages (`cli-utils`, `pkg/apis/meta`, `pkg/runtime`, `pkg/ssa`, `source-controller/api`) as the CLI's current dependencies and Flux's own current release line.
- Zero behavior change: no controller-observable difference in reconcile behavior, CRD shape, or RBAC.
- Confirm — not just assume — the bump is inert, via the existing validation gate stack (`dev:manifests`, `dev:generate`, `dev:fmt`, `dev:vet`, `dev:lint`, `dev:test`, optionally `dev:e2e`).

**Non-Goals:**

- No `go` directive change (not needed — see Context).
- No webhook work of any kind (none exists; none is being added).
- No change to `opm-operator`'s own API types, CRDs, or reconcile logic.
- Not attempting to also bump the CLI or `library` — this slice is `opm-operator`-only, per `enhancements/0006/planned-changes.md` A1.
- Not a vehicle for any *other* pending dependency bump beyond the k8s.io/controller-runtime/Flux set this change is now coupled to — still scoped per Principle VIII (Small Batch Sizes); the widening documented here is a forced coupling discovered during implementation, not an invitation to bundle unrelated upgrades.

## Decisions

### D1: Target `controller-runtime v0.24.1`, not `v0.24.0`

**Decision:** Pin `sigs.k8s.io/controller-runtime` to `v0.24.1`.

**Alternatives considered:** `v0.24.0` — the version that actually introduces the `k8s.io/* v0.36.0` bump. Rejected: v0.24.1 is a same-day-class bug-fix release over v0.24.0 (one cherry-picked fix for an SSA typed-error regression) with no additional breaking changes; there is no reason to pin the version with a known, already-fixed bug.

**Rationale:** Take the latest patch of the release that carries the intended k8s.io bump.

### D2: Do not bump the `go` directive

**Decision:** Leave `go 1.26.2` unchanged in `go.mod`.

**Alternatives considered:** Bump to match some other line, per the original A1 wording in `enhancements/0006/planned-changes.md` ("align the go directive"). Rejected: `opm-operator`'s directive (1.26.2) already exceeds both `controller-runtime v0.24.1`'s requirement (1.26.0) and the CLI's own directive (1.26.0) — there is nothing to align, and bumping further would be an unrelated, unmotivated change.

**Rationale:** Confirmed directly against the module's `go.mod` via the proxy rather than assumed; avoids unnecessary churn (Principle VII).

### D3: Regenerate manifests as a verification step, not because API types change

**Decision:** Run `task dev:manifests dev:generate` as part of this change and diff the output, even though no `api/v1alpha1` type is touched.

**Alternatives considered:** Skip regeneration since no types change. Rejected: `controller-gen`'s output can shift formatting or ordering across tool/dependency versions even with no schema change; regenerating and diffing is the cheap way to catch that rather than assume it's a no-op.

**Rationale:** Cheap insurance against silent generated-file drift; consistent with the repo's own Validation Gates.

### D4: Target Flux's own coordinated version set (the `flux2` v2.9.0 distribution's pins), not hand-picked minimums

**Decision:** Bump `fluxcd/cli-utils` → v1.2.2, `fluxcd/pkg/apis/meta` → v1.30.1, `fluxcd/pkg/runtime` → v0.110.1, `fluxcd/pkg/ssa` → v0.76.1, `fluxcd/source-controller/api` → v1.9.1 — the exact set that `github.com/fluxcd/flux2/v2@v2.9.0` (the real, released `flux` CLI/GitOps toolkit, the flagship consumer of every one of these libraries) pins in its own `go.mod`. That `go.mod` also pins `k8s.io/api v0.36.2` and `sigs.k8s.io/controller-runtime v0.24.1` — the same controller-runtime version this change independently arrived at, which is corroborating evidence this is the intended, tested combination rather than an arbitrary newer set of tags.

**Alternatives considered:** Hand-pick the minimum version of each Flux package that resolves the `go.mod` compile error (e.g., `cli-utils@v1.1.0`, `pkg/ssa@v0.74.0` — the first versions that pin `k8s.io/api v0.36.0`). Rejected: minimum-viable versions picked independently per package are *not* a combination Flux has actually released or tested together — `flux2` itself doesn't ship that exact mix. Riding the same set Flux's own distribution ships is strictly safer: it's the combination most likely to have been exercised by Flux's own test suite and real-world usage.

**Rationale:** When a forced upgrade already requires touching five coupled packages, picking the versions a real downstream distribution has already integrated and shipped is lower-risk than constructing a novel combination that satisfies MVS but has never been run together by anyone.

### D5: `cli-utils` v1.0.0's "reduce scope to `kstatus` only" breaking change does not apply

**Decision:** Treat `cli-utils`'s documented breaking change (release notes: "⚠️ Breaking change: Reduce the project scope to `kstatus` only") as a non-issue for this change, with no compensating code changes.

**Alternatives considered:** Audit for indirect uses of the removed (non-`kstatus`) `cli-utils` surface. Rejected as unnecessary: `grep` across `internal/`, `api/`, `cmd/` for `cli-utils` shows exactly one import site (`internal/apply/manager.go`, `fluxcd/cli-utils/pkg/kstatus/polling`) — the one subpackage the scope reduction *keeps*. There is no other surface to audit.

**Rationale:** Confirmed by direct code search rather than assumed from the changelog wording alone; the changelog's "breaking" label is accurate for `cli-utils`'s own consumers in general but inert for this specific one.

### D6: The remaining Flux package changes (ranges to the D4 pins) are additive/bugfix-only for the subpackages `opm-operator` imports

**Decision:** Treat the version ranges for `pkg/ssa`, `pkg/runtime`, and `source-controller/api` as behaviorally safe based on commit-level review, not just changelog headlines.

**Alternatives considered:** Trust the absence of a GitHub "Release" object with breaking-change notes for these ranges. Rejected: `fluxcd/pkg` is a monorepo where not every tag has a curated release note, so absence of a note is weak evidence. Instead, the commit history between the current pins and the D4 targets was filtered to the exact subpackages `opm-operator` imports:

- `pkg/ssa` (v0.69.0→v0.76.1): two bug fixes (`WaitForSetWithContext` timeout-when-empty, incorrect object reference on `SkippedAction`), two additive features (API-version-migration support, a new `DriftIgnoreRules` field on `ApplyOptions`), one test-only fix. Nothing changes existing-caller behavior for a caller not opting into the new options.
- `pkg/runtime` (v0.103.0→v0.110.1): the only in-range, subpackage-prefixed commits are in `runtime/cel` (CEL-based status-expression evaluation) — a subpackage `opm-operator` does not import (`internal/` only uses `runtime/conditions` and `runtime/patch`).
- `source-controller/api` (v1.8.0→v1.9.1): one change, an additive deepcopy regen for a new field (`TrustedRootSecretRef`).
- The recurring "⚠️ breaking API change" note across several of these compare ranges is in `pkg/git/repository` — a subpackage `opm-operator` never imports at all.

**Rationale:** A monorepo-wide changelog scan produces false alarms (unrelated subpackages) and false confidence (silent gaps) in roughly equal measure; filtering to the actual import surface is the only check that answers the real question — does this specific bump change this specific code's behavior.

### D7: What the e2e suite actually validates for this bump — corrected after actually running it

**Decision:** Rely on `task dev:e2e` only for what it actually, empirically demonstrated when run locally for this change — manager bootstrap and metrics-endpoint serving against a live Kind cluster on the bumped dependency set — and explicitly document that it provided **no** live coverage of `pkg/ssa`'s apply path, `cli-utils/pkg/kstatus/polling`'s readiness detection, or `source-controller/api`, contradicting this decision's own first-drafted version (below, kept for record per the append-only convention this file otherwise doesn't enforce, but worth keeping visible as a caution).

**What actually happened when `task dev:e2e` ran (2026-07-01):** `Ran 2 of 17 Specs — 2 Passed | 0 Failed | 0 Pending | 15 Skipped`. Only `test/e2e/e2e_test.go`'s "Manager" suite ("should run successfully", "should ensure the metrics endpoint is serving metrics") actually executed. Specifically:

- `test/e2e/prune_test.go` (all 5 scenarios) and `test/e2e/finalizer_test.go` (all 4 scenarios) are **also** pre-existing `Skip()`-stubbed TODOs — this was not checked before the first draft of this decision, which claimed they provided real coverage. They do not, independent of this bump.
- `test/e2e/podinfo_test.go` skipped for a different, real reason: its `BeforeAll` calls `Skip("example modules unresolvable: set LOCAL_REGISTRY (local) or OPERATOR_DOCKER_CONFIG (CI GHCR creds)")`, and neither was set for this local run. This is the one test in the suite that is *not* a permanent stub — `.github/workflows/test-e2e.yml` does set `OPERATOR_DOCKER_CONFIG` and publishes pre-release example modules to GHCR for same-repo pushes/PRs, so **this test should actually run for real in CI once this lands as a PR**, even though it did not run here. That is a real, load-bearing distinction: local verification for this change did not exercise `pkg/ssa`/`kstatus` at all; CI verification, when this becomes a PR, plausibly will — but that has not been confirmed from this session.
- `test/e2e/lifecycle_test.go` and `test/e2e/concurrent_test.go` (the `OCIRepository`-referencing files) are confirmed permanently `Skip()`-stubbed TODOs, as originally found — this part of the original research held up.
- Consistent with the `source-controller/api` gap: neither `.tasks/flux.yaml`'s `flux:install` task nor any Flux CLI install step appears anywhere in `test-e2e.yml`'s CI job — there is no running Flux `source-controller` to test against even if those two files' `Skip()`s were removed.
- Two environment variables remain unpinned in CI, pre-existing and not new: `test-e2e.yml` installs "the latest version of kind," and `Taskfile.yml`'s `FLUX` variable defaults to `PATH` for the local-only `flux:install` task.

**Alternatives considered:** Leave the original (wrong) D7 text in place since the suite did pass. Rejected: a passing `task dev:e2e` run and "this specifically validated the bumped SSA/kstatus code path" are different claims, and only the first one is true for this session's run. Silently leaving the stronger, false claim in place would misrepresent what was actually verified to anyone reading this later. Rerun `task dev:e2e:local` with `LOCAL_REGISTRY` wired up to force `podinfo_test.go` to actually execute now, rather than defer to CI. Considered but not done in this session: it requires the workspace's example modules already published to `localhost:5000` (a separate `modules/` task run), a real side-quest disproportionate to what's left in this change, and CI will exercise the identical path automatically once this lands as a PR — deferring to that is cheaper than reproducing it locally, provided the PR's CI result is actually checked before merge (see the task list).

**Rationale:** Getting this wrong once and correcting it here is a better outcome than not running the suite at all, but it's a direct lesson: reading `It(...)` scenario names and grepping for `Skip(` in two of five e2e files is not the same as confirming what a suite actually exercises. The fix isn't "don't claim e2e coverage" — it's "run it and read the actual tally before writing the claim," which is what this corrected version does. **This change's task list (3.4) now requires checking the PR's actual CI e2e result — specifically whether `podinfo_test.go` ran (not skipped) and passed — before merging, since that's the only place in the whole test matrix that would catch a real `pkg/ssa`/`kstatus` regression from this bump.**

### D8: Migrate off the deprecated `controller-runtime/pkg/scheme.Builder` — one small production code change, not zero

**Decision:** `task dev:lint` failed after the bump (`SA1019: scheme.Builder is deprecated`, from `api/v1alpha1/groupversion_info.go`) — controller-runtime v0.24.0 deprecated its `pkg/scheme.Builder` convenience wrapper (release notes: "Scheme: Deprecate the scheme builder"). Migrate to the replacement the deprecation notice itself specifies: `k8s.io/apimachinery/pkg/runtime.NewSchemeBuilder` plus explicit `scheme.AddKnownTypes`/`metav1.AddToGroupVersion` calls, rather than suppress the lint finding.

Four files touched, all mechanical, behavior-preserving (verified against `scheme.Builder`'s own source: it does exactly `scheme.AddKnownTypes(bld.GroupVersion, object...)` + `metav1.AddToGroupVersion(scheme, bld.GroupVersion)` per `Register` call — the replacement reproduces this precisely, just centralizing the `AddToGroupVersion` call once in `groupversion_info.go` instead of redundantly once per type file):

- `api/v1alpha1/groupversion_info.go` — `SchemeBuilder = &scheme.Builder{...}` → `runtime.NewSchemeBuilder(addToGroupVersion)`, with a new `addToGroupVersion` func doing the one `metav1.AddToGroupVersion` call.
- `api/v1alpha1/{moduleinstance,modulepackage,platform}_types.go` — each `init()`'s `SchemeBuilder.Register(&X{}, &XList{})` → `SchemeBuilder.Register(func(scheme *runtime.Scheme) error { scheme.AddKnownTypes(GroupVersion, &X{}, &XList{}); return nil })`.

**Alternatives considered:**

- Suppress with `//nolint:staticcheck` to keep this change's blast radius at zero production-code lines. Rejected: the replacement is small (4 files, ~15 lines), exactly the pattern the deprecating library itself documents, and non-behavior-changing (verified by reading `scheme.Builder`'s implementation, not assumed) — suppressing a correct, cheap, upstream-endorsed fix just to preserve a "zero code changes" label is exactly the kind of unnecessary caution the repo's own Principle VII (Simplicity & YAGNI, avoid unjustified complexity — including the complexity of a lingering suppression comment) argues against. A `nolint` would also just defer this to the next person who touches this file, with less context than exists right now.
- Leave `dev:lint` failing and treat it as a pre-existing/out-of-scope issue. Rejected: `dev:lint` passing is one of this repo's Validation Gates; a failing gate blocks merge regardless of whether this change or upstream "caused" it.

**Rationale:** `proposal.md`'s original "No production code changes are expected" claim doesn't survive contact with `task dev:lint` — corrected there. This is the one place in the whole bump where "no behavior change" required an actual (tiny, mechanical) code edit rather than just a version bump; everything else in this change is dependency-version-only.

**Source:** Discovered running task 3.2 (`task dev:lint`) during implementation; fixed in the same session per the "Fluid Workflow Integration" allowance (implementation revealing a small, unambiguous, correctly-scoped fix doesn't require a full pause).

## Risks / Trade-offs

- **[Risk] Undocumented client-go/apimachinery behavior change between v0.35 and v0.36** that neither the Kubernetes 1.36 nor client-go v0.36.0 release notes called out explicitly (they were searched and gave no complete deprecation/removal list at this granularity). → **Mitigation:** the existing validation gate stack (`dev:fmt dev:vet dev:lint dev:test`, plus `dev:e2e` if Kind is available) is the actual test of this; treat any test failure or lint regression as a signal to investigate before merging, not something to force through.
- **[Risk] Indirect dependency churn** (`sigs.k8s.io/apiserver-network-proxy/konnectivity-client`, `sigs.k8s.io/kustomize/api`, `sigs.k8s.io/kustomize/kyaml`, `sigs.k8s.io/structured-merge-diff/v6`, `k8s.io/kube-openapi`, etc.) moves versions underneath this bump without being the direct target. → **Mitigation:** let `go get`/`go mod tidy` resolve these via MVS naturally rather than hand-pinning each one; review the full `go.sum` diff for anything surprising (e.g., a major-version jump) before merging.
- **[Risk] Wider blast radius than originally scoped** — this change now touches five Flux packages in addition to `k8s.io/*`/`controller-runtime`, discovered mid-implementation rather than planned upfront. → **Mitigation:** D4's approach (ride Flux's own tested combination) plus D5/D6's subpackage-level review are the mitigation; still, this PR deserves a more careful review pass than the originally-scoped "trivial prep slice" — flag this explicitly in the PR description so reviewers don't under-scrutinize it based on the change's origin as a small prep task.
- **[Trade-off] This slice, by itself, unblocks nothing else in enhancement 0006** (`planned-changes.md` lists no dependents on A1) — it's pure technical-debt prevention, not feature-enabling work, and its original motivation (`research/findings.md` "Problem 3") was already soft even before this widening, since D13 (and later D31) removed the Go-level coupling between the CLI and `opm-operator` that made "keep both repos on one k8s line" more than a coherence nicety. Accepted regardless: having gone this far into the coupled Flux bump, finishing it now is cheaper than abandoning partial work and re-discovering the same coupling later.
- **[Risk] Downstream consumers pin their own Flux/operator versions independently of this change** — e.g. `opm-kind-demo`'s README states it pins "Flux toolkit version, opm-operator version... No surprise upgrades." Those pins don't move just because `opm-operator`'s internal Go dependencies do; they need their own coordinated check/bump once this change ships, and — per D7 — `opm-operator`'s own e2e suite provides no live coverage of the operator-vs-real-Flux-source-controller interaction to lean on for that check. → **Mitigation:** out of scope for this change by design (Principle VIII); tracked as a new cross-repo audit slice in `enhancements/0006/planned-changes.md` and a corresponding warning in `enhancements/0006/05-risks.md`, so it isn't silently dropped.

## Migration Plan

1. `go get k8s.io/api@v0.36.0 k8s.io/apiextensions-apiserver@v0.36.0 k8s.io/apimachinery@v0.36.0 k8s.io/client-go@v0.36.0 sigs.k8s.io/controller-runtime@v0.24.1 github.com/fluxcd/cli-utils@v1.2.2 github.com/fluxcd/pkg/apis/meta@v1.30.1 github.com/fluxcd/pkg/runtime@v0.110.1 github.com/fluxcd/pkg/ssa@v0.76.1 github.com/fluxcd/source-controller/api@v1.9.1` (indirect deps follow via MVS), then `go mod tidy`.
2. `task dev:manifests dev:generate` — diff `config/crd/bases/*.yaml`, `config/rbac/role.yaml`, `api/v1alpha1/zz_generated.deepcopy.go`; expect no diff, investigate if there is one.
3. `task dev:fmt dev:vet dev:lint dev:test` — all must pass unchanged.
4. `task dev:e2e` if a Kind cluster is available; note in the PR if skipped.
5. Single commit/PR — this is a self-contained, revertable dependency bump; no data migration, no CRD version change, so rollback is a plain `git revert` with no cluster-side cleanup required.

## Open Questions

None outstanding — see `enhancements/0006/03-decisions.md` OQ15/OQ16 for the separate, unrelated open questions this exploration surfaced (CLI/operator stale-set divergence; operator apply-time collision guard). Neither affects this change.
