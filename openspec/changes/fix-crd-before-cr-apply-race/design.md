## Context

`apply.Apply` (`internal/apply/apply.go:33-60`) is the only caller of Flux `ApplyAllStaged` (`fluxcd/pkg/ssa@v0.77.0`). Its result feeds both reconcilers' Apply phase:

- `internal/reconcile/moduleinstance.go:423`
- `internal/reconcile/modulepackage.go:496`

`ApplyAllStaged` (`manager_apply.go:347-420`) works in stages:

1. It applies cluster definitions: CRDs, Namespaces and ClusterRoles.
2. It calls `WaitForSet` (`:385`) on them with the `StatusPoller` from `internal/apply/manager.go:24`. kstatus calls a CRD Current once `Established=True` (`fluxcd/cli-utils`, `kstatus/status/core.go:601-627`).
3. It applies class definitions, then custom-stage kinds.
4. It applies everything else (`:413`).

In each `ApplyAll` the object is first read with `client.Get` (`:236`) and then dry-run applied (`:252-283`). A dry-run error comes back as `ssaerrors.DryRunErr`, which has `Unwrap() error`.

The client resolves a GVK through its RESTMapper. controller-runtime v0.24.1's dynamic mapper (`pkg/client/apiutil/restmapper.go`) re-reads discovery on every miss, through `addKnownGroupAndReload` (`:122-132`):

- With a version given, it fetches `/apis/<group>/<version>` (`:311-345`).
- A NotFound there drops the group from its caches and caches nothing negative, so the next call fetches again.
- On the aggregated-discovery path (`:187-201`) the miss comes back as `*apiutil.ErrResourceDiscoveryFailed`. That unwraps to `*meta.NoResourceMatchError` (`errors.go:45-54`).
- `meta.IsNoMatchError` uses `errors.Is` (apimachinery v0.36.4, `api/meta/errors.go:127-132`), so it matches through every one of these wrappers.

The failing CI run (Tests 37176383926, attempt 1, envtest 1.36.2, Ginkgo seed 1791087360) shows that discovery can still miss the new kind after `Established`:

- The spec ran 21st of 21 and failed in 30 ms.
- The error was a `DryRunErr` wrapping `*meta.NoKindMatchError{Group: "test.example.com", Kind: "Widget", SearchedVersions: ["v1"]}`.
- The first `Get` had already missed, and so had the dry run's own lookup a few milliseconds later.

Reconcile phase impact: **Apply** only. The phase gets a bounded internal retry, and its result counts merge across attempts. Source, Render, Prune and Inventory are untouched. **Status:** on the path that hits the race, the reconcile now reaches `Ready=True` on the first attempt. It no longer reports a transient `ApplyFailed`, with its Warning event, its `failureCounters.reconcile` increment and a backoff requeue. Every other apply failure reports exactly as today.

## Goals / Non-Goals

**Goals:**

- A resource set that contains a CRD and instances of it applies in one `Apply` call, even when discovery lags `Established` by up to 10 s.
- Every other failure surfaces exactly as today, with no added delay.
- The apply integration suite has no timing-dependent failure in this spec, and a deterministic test guards the fix.

**Non-Goals:**

- Changing Flux's staging or its `WaitForSet` semantics, or forking `ApplyAllStaged`.
- Discovery lag for a CRD that already existed and gained a new served version in this apply. The no-match carries the kind, and the CRD is in the set, so the retry covers it as a side effect. No requirement promises it.
- The cli's apply path (see Risks).
- A RESTMapper reset or a shared mapper cache.

## Research & Decisions

### D1. Fix the product code, not only the test

**Context.** The race sits in the production apply path. Both reconcilers would surface it as a transient `ApplyFailed` on a module's first apply.

**Explored.**
- The research note (Phase 3 follow-ups, item 1).
- `ApplyAllStaged` and `WaitForSet` in ssa v0.77.0.
- kstatus `crdConditions`.
- The controller-runtime v0.24.1 dynamic RESTMapper.
- `git log -- internal/apply/apply.go test/integration/apply/apply_test.go`: 7999ace, 59d6e66, 73e8c32. None touches discovery or no-match handling.
- The other flake fixes on main (e234231, 1505bea) concern reconcile-inventory polling and are unrelated.

Options:
1. Wrap the test's `apply.Apply` call (`apply_test.go:224`) in `Eventually`. This hides the production gap.
2. Reset the mapper after the CRD stage. The mapper already reloads on a miss (`restmapper.go:122-132`), so a reset changes nothing while discovery lags.
3. A bounded retry inside `apply.Apply`, limited to a no-match whose kind a CRD in the same set defines.

**Decision.** Option 3.

**Rationale.**
- It fixes the user-visible spurious failure, and the test then needs no change to its assertions.
- Limiting the retry to kinds defined in the set means a module whose CRD is genuinely missing is never delayed.

### D2. The retry predicate

**Context.** The retry must not fire for anything but "this set's own CRD is not served yet".

**Decision.** `Apply` retries when all of these hold:

- `meta.IsNoMatchError(err)` is true.
- `errors.As` finds a `*meta.NoKindMatchError` whose `GroupKind` equals a set CRD's `spec.group` and `spec.names.kind`, or a `*meta.NoResourceMatchError` whose `PartialResource.Group` equals a set CRD's `spec.group`. The second form is the aggregated-discovery path, which carries no kind.
- The requirement states both forms: a kind that a set CRD defines, or, when discovery reports only the group, a group that a set CRD defines. A group-only match can retry a kind of that group that no set CRD defines; that costs at most the bound, and only for a set that ships a CRD of that group.

`errors.As` walks `Unwrap() []error` (Go 1.20+), so it reaches through `fmt.Errorf("%w")`, `DryRunErr` and `ErrResourceDiscoveryFailed`.

```go
// pendingCRDKind reports whether err only says the API server does not serve
// a kind yet that a CustomResourceDefinition in resources defines: discovery
// lags a CRD's Established condition, so the custom resource is retried.
func pendingCRDKind(err error, resources []*unstructured.Unstructured) bool {
	if !meta.IsNoMatchError(err) {
		return false
	}
	defined := crdKinds(resources) // map[schema.GroupKind]struct{} from spec.group + spec.names.kind
	if len(defined) == 0 {
		return false
	}
	if kindErr, ok := errors.AsType[*meta.NoKindMatchError](err); ok {
		_, found := defined[kindErr.GroupKind]
		return found
	}
	if resErr, ok := errors.AsType[*meta.NoResourceMatchError](err); ok {
		for gk := range defined {
			if gk.Group == resErr.PartialResource.Group {
				return true
			}
		}
	}
	return false
}
```

`crdKinds` reads only objects whose GVK is `apiextensions.k8s.io/v1 CustomResourceDefinition`, through `unstructured.NestedString`. A CRD with a missing group or kind contributes nothing. `errors.AsType` needs Go 1.26. The repo is on `go 1.26.2` (`go.mod:3`) and already uses it (`internal/controller/platform_controller.go:480`).

**Rationale.** The predicate is a pure function of the error and the input, so it can be tested in a table without envtest.

### D3. The retry loop and its bound

**Decision.** `Apply` hands a closure over `rm.ApplyAllStaged` to an internal `applyWithDiscoveryRetry`, so the loop can be unit tested with a stub instead of a `ResourceManager`:

```go
// The bound and interval are variables so the package's unit tests can shorten them.
// Tests that shorten them must not call t.Parallel.
var (
	discoveryRetryInterval = 500 * time.Millisecond
	discoveryRetryTimeout  = 10 * time.Second
)

type stagedApply func(ctx context.Context) (*fluxssa.ChangeSet, error)

func applyWithDiscoveryRetry(ctx context.Context, apply stagedApply, resources []*unstructured.Unstructured) (*ApplyResult, error) {
	ledger := actionLedger{} // first created or configured action per object
	var pending error            // the last retryable no-match
	var deadline time.Time       // set at the first retryable failure
	for {
		cs, err := apply(ctx) // every attempt gets the caller's ctx, unchanged
		ledger.record(cs)     // cs holds the entries applied up to a failure; nil-safe
		if err == nil {
			return ledger.result(cs), nil
		}
		if pending != nil && ctx.Err() != nil &&
			(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || pendingCRDKind(err, resources)) {
			return nil, fmt.Errorf("failed to apply resources: %w", pending)
		}
		if !pendingCRDKind(err, resources) {
			return nil, fmt.Errorf("failed to apply resources: %w", err)
		}
		pending = err
		if deadline.IsZero() {
			deadline = time.Now().Add(discoveryRetryTimeout)
		}
		logf.FromContext(ctx).V(1).Info("Waiting for the API server to serve a custom resource kind", "error", err.Error())
		timer := time.NewTimer(discoveryRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("failed to apply resources: %w", pending)
		case <-timer.C:
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to apply resources: %w", pending)
		}
	}
}
```

- **The bound limits when a new attempt may start, not how long an attempt may run.** Each attempt runs under the caller's `ctx`, exactly as the single call does today. A large module, a slow API server, a CRD stage that waits several seconds or a force-recreate whose finalizer holds keeps today's behaviour. The first draft used `wait.PollUntilContextTimeout`, which wraps `ctx` in a 10 s `context.WithTimeout` and hands that to every attempt, the first one included (apimachinery v0.36.4 `pkg/util/wait/poll.go:45-48`); it would have added a new `ApplyFailed` on any apply that runs past 10 s. A unit test with a stub that records whether its context has a deadline guards this.
- **Interval and bound.** 500 ms and 10 s. The observed lag is milliseconds. 10 s bounds a reconcile's extra time in the worst case and is far below the 60 s `WaitTimeout` that Flux already allows the CRD stage (`DefaultApplyOptions`, `manager_apply.go:111-118`).
- **The caller's context ends the retry too.** The wait between attempts selects on `ctx.Done()`.
- **On the bound or a context end** after a retryable error, `Apply` returns the last no-match error, wrapped as today (`failed to apply resources: ...`). That includes an attempt that the context cut short in flight: its error is a context error, not the cause, so the earlier no-match is returned. An attempt that the context cut short after it hit a real error, such as a conflict once discovery served the kind, returns that error, not the old no-match. The reconcile then reports `ApplyFailed` with the message it gives today. A first attempt that fails keeps its own error, whatever it is.
- **No new log at Info.** The V(1) line, once per retry, follows the logging rules in `AGENTS.md` (capitalised message, structured keys).
- The first attempt starts at once, so the common path costs nothing.

### D4. Counting across attempts

**Context.** A retry re-runs the whole set. On the second attempt the CRD dry-run compares equal and reports `UnchangedAction`. A naive count would turn "2 created" into "1 created, 1 unchanged", and the `Applied N resources (...)` event and log would misreport. The existing spec asserts `result.Created == 2` (`apply_test.go:226`).

**Decision.**
- `ApplyAllStaged` returns the partial change set together with its error (`manager_apply.go:379-416`: every error return passes `changeSet`).
- `Apply` keeps a ledger keyed by `ChangeSetEntry.ObjMetadata`. It stores the first `CreatedAction` or `ConfiguredAction` seen for an object. An object only ever seen as `UnchangedAction` counts as Unchanged.
- The counts come from the ledger after the successful attempt.
- Only objects in the successful attempt's change set are counted. That attempt's change set covers every input object, so nothing is dropped and nothing is double-counted.

### D5. Reproduction: a simulated lag for the retry, a stress spec for the race

**Context.** On this host the race did not reproduce naturally: 264 local runs of the unchanged suite, 0 failures. Those planning runs used envtest 1.36.0 from the main checkout's `bin/k8s`; the failing CI run used 1.36.2 (CI log, `bin/k8s/1.36.2-linux-amd64`). They are:

| Load | Runs | Failures |
| --- | --- | --- |
| serial | 30 | 0 |
| `xargs -P 10` | 30 | 0 |
| `-P 24` | 64 | 0 |
| CI seed 1791087360, `-P 16` | 100 | 0 |
| `taskset -c 0`, `-P 6` | 40 | 0 |

The loop scripts are `p3-operator-flaky-loop.sh` and `p3-operator-flaky-par.sh` in the supervisor scratchpad. Every run recorded from here on uses the envtest version that `task dev:test` resolves in the worktree (`ENVTEST_K8S_VERSION` 1.36, which resolves to 1.36.2, as in CI).

**Decision.** Two harnesses, with different jobs:

- **A lagging RESTMapper tests the retry logic, not the race.** The integration suite gets a `laggingMapper`, a `meta.RESTMapper` wrapper. For one configured `GroupKind` it returns `&meta.NoKindMatchError{...}` from `RESTMapping` and `RESTMappings` until a lag window has passed since the first lookup of that kind. Every other kind delegates to the real mapper from `apiutil.NewDynamicRESTMapper(cfg, httpClient)`. A per-spec client built with `client.New(cfg, client.Options{Mapper: lagging})` feeds `apply.NewResourceManager`. The kstatus poller also uses `c.RESTMapper()`, but it only looks up the CRD's own kind, which is never lagged. Before the fix, an `Apply` of `{gadget, crd}` under a 1 s lag fails with the CI error shape (`no matches for kind "Gadget" in version "lag.example.com/v1"`); after it, the call succeeds with `Created == 2` and takes at least the lag. That the spec fails every time before the fix only proves the fake returns the error it is built to return.
- **A natural stress spec tries for the real race.** One `Apply` of 20 CRDs, each in a fresh group, plus one instance of each, through the real mapper. Each apply gives the discovery window 20 chances. It runs in loops before and after the fix.

Section 1 lands the lagging mapper with a spec that asserts today's failure, and the stress spec. Section 2 flips the lag spec to assert success when it lands the fix.

**Runs before the fix** (envtest 1.36.2, loop script `p3-operator-flaky-runs.sh before` in the supervisor scratchpad; a serial run is about 5.7 s, mostly envtest start-up):

| Spec | Load | Runs | Failures |
| --- | --- | --- | --- |
| lag spec (asserts today's no-match) | serial | 30 | 0 (the no-match appeared 30 of 30 times, as the fake guarantees) |
| stress spec (20 CRDs and instances) | serial | 30 | 0 |
| Widget spec | serial | 30 | 0 |
| whole suite, stress spec included | `xargs -P 24` | 64 | 0 |

The stress spec gave the discovery window 1,880 chances (94 applies of 20 kinds) on the CI envtest version and never lost the race. The race did not reproduce naturally on this host.

**Runs after the fix** (same envtest, `p3-operator-flaky-runs.sh after`):

| Spec | Load | Runs | Failures | Time for 30 serial runs (before → after) |
| --- | --- | --- | --- | --- |
| lag spec (now asserts success under a 1 s lag) | serial | 30 | 0 | 168.6 s → 206.2 s, the 1 s lag plus a retry tick per run |
| stress spec | serial | 30 | 0 | 178.4 s → 191.4 s |
| Widget spec | serial | 30 | 0 | 172.5 s → 177.6 s |
| whole suite | `xargs -P 24` | 64 | 0 | |

The V(1) retry line shows under `-ginkgo.v` (2 retries in a lag spec run), and 15 more verbose runs of the stress and Widget specs logged no retry at all, so the retry never fired outside the lag spec. The stress and Widget time differences are run-to-run noise in envtest start-up, not retry time.

**What the evidence shows.** The stress loop never reproduced the race, before or after the fix. So the evidence that the fix closes the real window is the code-path argument (Context, D2) plus the single CI log, and nothing more. The lag spec proves the retry handles the error shape that log shows, and the after runs show the fix changes nothing when discovery keeps up.

## Risks / Trade-offs

- **Up to 10 s of extra reconcile time** when a set's CRD never becomes served, for example when the CRD is Established but its version is `served: false`. Previously this failed at once. The error and reason are unchanged. The 10 s is bounded and only applies to a set that contains the defining CRD (or, on the group-only path, a CRD of the same group). Each of the about 20 attempts in that window re-runs the whole CRD stage (a Get and a dry run per CRD, then `WaitForSet`), so a module with N CRDs makes about 20 × 2N extra API calls per failed reconcile, on every requeue. That cost is accepted: it is bounded, and it only arises for a CRD that is Established but never served.
- **A whole-set retry repeats the dry runs of already-applied objects.** SSA is idempotent, and the dry runs are cheap compared with a reconcile requeue. The CRD stage's `WaitForSet` returns at once on the retry, because the CRD is already Established.
- **The lag mapper tests the retry, not the API server.** See D5. The real window is exercised only by the stress spec and the existing Widget spec (`apply_test.go:176`) staying in the suite.
- **The cli does not share this race.** Checked on cli main 19f19cd2: `internal/kubernetes/apply.go` `applyOne` builds the GroupVersionResource from the kind by name (`GVRFromUnstructured`, `KindToResource` in `resource.go`) and calls the dynamic client directly. No client-side RESTMapper or discovery lookup sits between its Established wait (`waitEstablished`) and the custom resource's request, so the client-side discovery miss this change fixes cannot happen there. No cli issue is filed.
- **The ssa library could change `ApplyAllStaged`'s partial change set contract.** D4 relies on `manager_apply.go:379-416` returning `changeSet` with every error. A Dependabot bump of `fluxcd/pkg/ssa` that changes this would make the counts on the retry path wrong, but not the apply itself. The D4 integration assertion (`Created == 2` under lag) would catch it.

## Sections

1. **Spike: reproduction harnesses (test only).** The lagging mapper with a spec that asserts today's failure under a simulated lag, and the natural stress spec. This records the reproduction. It ends green and ships nothing.
2. **Fix: bounded discovery retry in `apply.Apply`.** It adds the predicate, the loop and the ledger. The lag spec flips to success. It adds the no-retry and context-end specs and the unit tests for the predicate, the loop and the ledger. It runs the before and after loops and records them here. The commit is `fix(apply): ...`. Then `openspec verify` and the archive ride the same PR.

Two sections, one PR (the default). The PR title is `fix(apply): wait for discovery of a CRD applied in the same set`.
