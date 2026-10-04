# Tasks: fix-crd-before-cr-apply-race

The sections follow design.md § Sections. No API types change, so `task dev:manifests dev:generate` is not part of any gate.

Every envtest run below exports an absolute `KUBEBUILDER_ASSETS`, for example `$(./bin/setup-envtest use 1.36.0 --bin-dir ./bin -p path)`. In a worktree, use the main checkout's `bin/`. Ginkgo refuses `go test -count=N`, so "N runs" means building the suite once and running it N times:

```sh
go test -c -o /tmp/apply.test ./test/integration/apply   # in a scratch directory
for i in $(seq 1 30); do ( cd test/integration/apply && /tmp/apply.test -ginkgo.focus "<focus>" ) || echo "run $i FAILED"; done
```

Run the binary from `test/integration/apply`. The suite finds envtest through `../../../bin/k8s` when `KUBEBUILDER_ASSETS` is unset.

## 1. Spike: reproduce the discovery race deterministically (test/integration/apply)

- [ ] 1.0 Repair the shape of the main spec `openspec/specs/ssa-apply/spec.md`. Today its three requirements sit outside a `## Requirements` section, so `openspec validate ssa-apply --type spec --strict` fails, and archive would refuse this change's delta ("target spec is structurally invalid").
  - Add a `## Purpose` paragraph: the capability applies rendered resources with Server-Side Apply as `opm-controller`, in Flux's stages, and reports created, updated and unchanged counts.
  - Put a `## Requirements` heading above the first requirement.
  - Change no requirement or scenario text.
  - Verify: `openspec validate ssa-apply --type spec --strict` passes, and `openspec validate fix-crd-before-cr-apply-race --strict` no longer prints the "Archive would refuse this delta" notice.
- [ ] 1.1 In `test/integration/apply/suite_test.go`, add `laggingMapper` per design.md D5.
  - It is a `meta.RESTMapper` that wraps a real mapper, `apiutil.NewDynamicRESTMapper(cfg, httpClient)` with `rest.HTTPClientFor(cfg)`.
  - It takes one `schema.GroupKind` and a lag `time.Duration`.
  - `RESTMapping` and `RESTMappings` for that GroupKind return `&meta.NoKindMatchError{GroupKind: gk, SearchedVersions: versions}` until the lag has passed since the first lookup of that GroupKind.
  - Every other method and GroupKind delegates.
  - Add a helper `newLaggingResourceManager(gk, lag) *fluxssa.ResourceManager`. It builds `client.New(cfg, client.Options{Mapper: m})` and passes it to `apply.NewResourceManager`.
  - Doc comments say it simulates API discovery lagging a CRD's `Established` condition.
- [ ] 1.2 In `test/integration/apply/apply_test.go`, add a Context "When discovery serves a new CRD's kind late" with a spec "reproduces the no-match failure of a single staged apply".
  - It uses its own CRD and kind (`gadgets.lag.example.com`, `Gadget`), so it shares no state with the Widget spec.
  - It uses a 1 s lag on `lag.example.com/Gadget`, and calls `apply.Apply(ctx, rm, {gadget, crd}, false)`.
  - It asserts that the error satisfies `meta.IsNoMatchError` and that its message contains `no matches for kind "Gadget" in version "lag.example.com/v1"`. That is the CI shape of Tests run 37176383926.
  - It cleans up the CRD and waits for it to be gone, using `Eventually` (no sleeps).
  - Move the spec reference comment at `apply_test.go:173-174` to `openspec/specs/ssa-apply/spec.md`.
- [ ] 1.3 Reproduction proof, before the fix. Run the 1.2 spec 30 times (focus `reproduces the no-match failure`).
  - Verify: 30 of 30 pass, which means the race reproduced 30 of 30 times.
  - Run the unchanged Widget spec (focus `apply the CRD before the custom resource`) 30 times serially.
  - Record both counts in design.md D5, next to the 264-run baseline.
- [ ] 1.4 Run `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(apply): reproduce the CRD discovery race with a lagging RESTMapper`.

## 2. Fix: bounded discovery retry in apply.Apply (internal/apply)

- [ ] 2.1 Red first, in a new `internal/apply/apply_test.go` (package `apply`), as table tests for `pendingCRDKind` (design.md D2). Rows:
  - true for a `DryRunErr`-like `fmt.Errorf("...: %w", &meta.NoKindMatchError{...})` whose GroupKind a CRD in the set defines.
  - true for a `*apiutil.ErrResourceDiscoveryFailed` holding a NotFound for the CRD's group.
  - false for a NoKindMatch of a kind no set CRD defines.
  - false when the set has no CRD.
  - false for a non-no-match error such as `apierrors.NewConflict`.
  - false for a CRD object missing `spec.names.kind`.

  Add a ledger test for D4: a first change set with the CRD `Created` and the second with it `Unchanged` and the instance `Created` must count Created 2.

  Verify: with stubs for `pendingCRDKind` that return false and an empty ledger, `go test ./internal/apply -run 'PendingCRDKind|Ledger'` fails on the true rows and the ledger row. Record the failing output.
- [ ] 2.2 In `internal/apply/apply.go`, implement `crdKinds`, `pendingCRDKind`, the action ledger and the retry loop of design.md D3:
  - `discoveryRetryInterval = 500 * time.Millisecond` and `discoveryRetryTimeout = 10 * time.Second`.
  - `wait.PollUntilContextTimeout` with `immediate=true`.
  - Return the last apply error, wrapped `failed to apply resources: %w`.
  - One `V(1)` log line through `logf.FromContext(ctx)` with a capitalised message.

  Rewrite the `Apply` doc comment. It states the stage model inline (no `docs/design/flux-ssa-staging.md`) and the discovery retry with its bound.

  Verify: `go test ./internal/apply` passes.
- [ ] 2.3 In `test/integration/apply/apply_test.go`:
  - Flip the 1.2 spec to "applies the custom resource once discovery serves its kind". Under the 1 s lag, `Apply` succeeds, `result.Created` is 2, the call took at least the lag, and the Gadget exists.
  - Add "fails at once for a custom resource whose CRD is not in the set". A `Gadget2` of an unserved group with no CRD in the set returns a no-match error in under 2 s.
  - Add "returns the no-match error when discovery never serves the kind". Use a lag of 1 h and a `context.WithTimeout` of 2 s. The error wraps a NoKindMatch for the kind and `meta.IsNoMatchError` holds. The test does not wait for the 10 s bound.

  Verify: `go test ./test/integration/apply/...` passes.
- [ ] 2.4 Proof after the fix:
  - Run the flipped spec 30 times. Verify 30 of 30 pass.
  - Run the Widget spec (focus `apply the CRD before the custom resource`) 30 times serially and the whole suite 64 times at `xargs -P 24`. Verify 0 failures.
  - Record the counts in design.md D5 beside the before counts.
- [ ] 2.5 Update `openspec/changes/fix-crd-before-cr-apply-race/design.md` Risks if the run times in 2.4 show the retry adding time to the unlagged Widget spec. It should add none, because `immediate=true` makes the first attempt synchronous.
- [ ] 2.6 Run `task dev:fmt dev:vet dev:lint dev:test` green, then commit `fix(apply): wait for discovery of a CRD applied in the same set`. The body says:
  - A CRD can be Established before discovery serves its kind, so a custom resource in the same set failed its dry run with "no matches for kind".
  - `apply.Apply` now retries the staged apply for up to 10s, only for a kind that a CRD in the set defines.
  - Counts keep the first attempt's created or configured action.
