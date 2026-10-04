# Tasks: fix-crd-before-cr-apply-race

The sections follow design.md § Sections. No API types change, so `task dev:manifests dev:generate` is not part of any gate.

Every envtest run below uses the version `task dev:test` resolves (`ENVTEST_K8S_VERSION`, today 1.36, which resolves to 1.36.2 as in CI), never a hard-coded one: export an absolute `KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.36 --bin-dir ./bin -p path)"` from the worktree root, whose `bin/` holds 1.36.2 after one `task dev:test`. Ginkgo refuses `go test -count=N`, so "N runs" means building the suite once and running it N times, with the binary in a scratch directory:

```sh
B="$(mktemp -d)/apply.test"
go test -c -o "$B" ./test/integration/apply
for i in $(seq 1 30); do ( cd test/integration/apply && "$B" -ginkgo.focus "<focus>" ) || echo "run $i FAILED"; done
```

Run the binary from `test/integration/apply`.

## 1. Spike: reproduction harnesses (test/integration/apply)

- [x] 1.0 Repair the shape of the main spec `openspec/specs/ssa-apply/spec.md`. Today its three requirements sit outside a `## Requirements` section, so `openspec validate ssa-apply --type spec --strict` fails, and archive would refuse this change's delta ("target spec is structurally invalid").
  - Add a `## Purpose` paragraph: the capability applies rendered resources with Server-Side Apply as `opm-controller`, in Flux's stages, and reports created, updated and unchanged counts.
  - Put a `## Requirements` heading above the first requirement.
  - Change no requirement or scenario text.
  - Verify: `openspec validate ssa-apply --type spec --strict` passes, and `openspec validate fix-crd-before-cr-apply-race --strict` no longer prints the "Archive would refuse this delta" notice.
- [x] 1.1 In `test/integration/apply/suite_test.go`, add `laggingMapper` per design.md D5.
  - It is a `meta.RESTMapper` that wraps a real mapper, `apiutil.NewDynamicRESTMapper(cfg, httpClient)` with `rest.HTTPClientFor(cfg)`.
  - It takes one `schema.GroupKind` and a lag `time.Duration`.
  - `RESTMapping` and `RESTMappings` for that GroupKind return `&meta.NoKindMatchError{GroupKind: gk, SearchedVersions: versions}` until the lag has passed since the first lookup of that GroupKind.
  - Every other method and GroupKind delegates.
  - Add a helper `newLaggingResourceManager(gk, lag) *fluxssa.ResourceManager`. It builds `client.New(cfg, client.Options{Mapper: m})` and passes it to `apply.NewResourceManager`.
  - Doc comments say it simulates API discovery lagging a CRD's `Established` condition.
- [x] 1.2 In `test/integration/apply/apply_test.go`, add a Context "When discovery serves a new CRD's kind late" with a spec "reproduces the no-match failure of a single staged apply".
  - It uses its own CRD and kind (`gadgets.lag.example.com`, `Gadget`), so it shares no state with the Widget spec.
  - It uses a 1 s lag on `lag.example.com/Gadget`, and calls `apply.Apply(ctx, rm, {gadget, crd}, false)`.
  - It asserts that the error satisfies `meta.IsNoMatchError` and that its message contains `no matches for kind "Gadget" in version "lag.example.com/v1"`. That is the CI shape of Tests run 37176383926.
  - It cleans up the CRD and waits for it to be gone, using `Eventually` (no sleeps).
  - Move the spec reference comment at `apply_test.go:173-174` to `openspec/specs/ssa-apply/spec.md`.
- [x] 1.3 In the same file, add a Context "When applying many new CRDs and their instances together" with a spec "applies every custom resource in one call". It applies 20 CRDs, each in a fresh group (`s<i>.stress.example.com`, kind `Stress<i>`), plus one instance of each, in one `apply.Apply` through the suite's real mapper, and asserts success and `Created == 40`. It deletes the CRDs and waits for them to be gone.
- [x] 1.4 Reproduction runs, before the fix, on envtest 1.36.2.
  - Run the 1.2 spec 30 times. 30 of 30 pass. This shows only that the fake mapper returns the error it is built to return (design.md D5), not that the race is real.
  - Run the 1.3 stress spec 30 times serially and the whole suite 64 times at `xargs -P 24`, to try for a natural reproduction.
  - Run the unchanged Widget spec (focus `apply the CRD before the custom resource`) 30 times serially.
  - Record every count in design.md D5, next to the 264-run baseline. If the stress spec reproduces the race, it cannot be committed green in this section: move it to section 2 and record that.
- [x] 1.5 Run `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(apply): reproduce the CRD discovery race with a lagging RESTMapper`.

## 2. Fix: bounded discovery retry in apply.Apply (internal/apply)

- [x] 2.1 Red first, in a new `internal/apply/apply_test.go` (package `apply`).
  - Table tests for `pendingCRDKind` (design.md D2). Rows:
    - true for a `DryRunErr`-like `fmt.Errorf("...: %w", &meta.NoKindMatchError{...})` whose GroupKind a CRD in the set defines.
    - true for a `*apiutil.ErrResourceDiscoveryFailed` holding a NotFound for the CRD's group.
    - false for a NoKindMatch of a kind no set CRD defines.
    - false when the set has no CRD.
    - false for a non-no-match error such as `apierrors.NewConflict`.
    - false for a CRD object missing `spec.names.kind`.
  - A ledger test for D4: a first change set with the CRD `Created` and the second with it `Unchanged` and the instance `Created` must count Created 2.
  - Loop tests for `applyWithDiscoveryRetry` (design.md D3) with a stub `stagedApply`:
    - Every attempt, the first and each retry, sees a context without a deadline when the caller's has none.
    - A no-match for a set CRD's kind, then success: two attempts, no error.
    - The bound ends the retry: with `discoveryRetryTimeout` and `discoveryRetryInterval` shortened, a stub that always returns the no-match makes `Apply` return an error with `meta.IsNoMatchError` true and more than one attempt, while the caller's context is still live.
    - The caller's context ends the retry: the error wraps the no-match, including when the in-flight attempt returns a context error.
    - The caller's context ends after an attempt hit a real error (a conflict): the error wraps the conflict, not the earlier no-match.
    - A non-no-match error and a no-match for a kind outside the set: one attempt each.

  Verify: with stubs for `pendingCRDKind` that return false and an empty ledger, `go test ./internal/apply -run 'PendingCRDKind|Ledger|DiscoveryRetry'` fails on the true rows, the ledger row and the retry tests. Record the failing output.
- [x] 2.2 In `internal/apply/apply.go`, implement `crdKinds`, `pendingCRDKind`, the action ledger and the retry loop of design.md D3:
  - Package variables `discoveryRetryInterval = 500 * time.Millisecond` and `discoveryRetryTimeout = 10 * time.Second`.
  - A plain loop: every attempt gets the caller's `ctx`; the bound starts at the first retryable failure and limits only when a new attempt may start; the wait selects on `ctx.Done()`.
  - Return the last no-match error, wrapped `failed to apply resources: %w`, when the bound or the context ends the retry.
  - One `V(1)` log line per retry through `logf.FromContext(ctx)` with a capitalised message.

  Rewrite the `Apply` doc comment. It states the stage model inline (no `docs/design/flux-ssa-staging.md`) and the discovery retry with its bound.

  Verify: `go test ./internal/apply` passes.
- [x] 2.3 In `test/integration/apply/apply_test.go`:
  - Flip the 1.2 spec to "applies the custom resource once discovery serves its kind". Under the 1 s lag, `Apply` succeeds, `result.Created` is 2, the call took at least the lag, and the Gadget exists.
  - Add "fails at once for a custom resource whose CRD is not in the set". The set holds a custom resource of an unserved group and kind plus an unrelated CRD (a different group and kind), so the GroupKind match is exercised, not the empty-set short-circuit. The spec establishes the unrelated CRD first, so only the failure path is timed, and `Apply` returns a no-match error in under 5 s, half the retry bound (2 s left too little room for a loaded CI host).
  - Add "returns the no-match error when the context ends before discovery serves the kind". Use its own kind (`never.example.com`, `Doohickey`), establish the CRD first so the deadline covers only the retry, then a lag of 1 h and a `context.WithTimeout` of 1750 ms, between retry ticks. The error wraps a NoKindMatch for the kind and `meta.IsNoMatchError` holds. The 10 s bound itself is covered by the 2.1 unit test.

  Verify: `go test ./test/integration/apply/...` passes.
- [x] 2.4 Runs after the fix, on envtest 1.36.2:
  - Run the flipped spec 30 times. Verify 30 of 30 pass.
  - Run the stress spec and the Widget spec (focus `apply the CRD before the custom resource`) 30 times serially each, and the whole suite 64 times at `xargs -P 24`. Verify 0 failures.
  - Record the counts in design.md D5 beside the before counts, and state what they show and do not show (D5, "What the evidence shows").
- [x] 2.5 Update design.md Risks if the run times in 2.4 show the retry adding time to the unlagged Widget spec. It should add none, because the first attempt starts at once.
- [x] 2.6 Run `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(apply): wait for discovery of a CRD applied in the same set`. The body says:
  - A CRD can be Established before discovery serves its kind, so a custom resource in the same set failed its dry run with "no matches for kind".
  - `apply.Apply` now retries the staged apply, starting no new attempt after 10s, only for a kind that a CRD in the set defines; each attempt keeps the caller's context.
  - Counts keep the first attempt's created or configured action.
- [ ] 2.7 Run `openspec verify` for the change (the repo's verify skill), then `openspec archive fix-crd-before-cr-apply-race --yes`, confirm `openspec validate ssa-apply --type spec --strict` passes, and commit the archive inside the same PR.
