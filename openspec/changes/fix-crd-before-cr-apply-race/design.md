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

**Decision.**

```go
const (
	discoveryRetryInterval = 500 * time.Millisecond
	discoveryRetryTimeout  = 10 * time.Second
)

func Apply(ctx context.Context, rm *fluxssa.ResourceManager, resources []*unstructured.Unstructured, force bool) (*ApplyResult, error) {
	opts := fluxssa.DefaultApplyOptions()
	opts.Force = force

	acc := newActionLedger() // first non-Unchanged action per object
	var lastErr error
	pollErr := wait.PollUntilContextTimeout(ctx, discoveryRetryInterval, discoveryRetryTimeout, true,
		func(ctx context.Context) (bool, error) {
			cs, err := rm.ApplyAllStaged(ctx, resources, opts)
			acc.record(cs) // cs holds the entries applied up to a failure; nil-safe
			if err == nil {
				lastErr = nil
				return true, nil
			}
			lastErr = err
			if pendingCRDKind(err, resources) {
				logf.FromContext(ctx).V(1).Info("Waiting for the API server to serve a custom resource kind", "error", err.Error())
				return false, nil
			}
			return false, err // not retryable: stop now
		})
	if lastErr != nil {
		return nil, fmt.Errorf("failed to apply resources: %w", lastErr)
	}
	if pollErr != nil { // context ended before any attempt finished
		return nil, fmt.Errorf("failed to apply resources: %w", pollErr)
	}
	return acc.result(), nil
}
```

- **Interval and cap.** 500 ms and 10 s. The observed lag is milliseconds. 10 s bounds a reconcile's extra time in the worst case and is far below the 60 s `WaitTimeout` that Flux already allows the CRD stage (`DefaultApplyOptions`, `manager_apply.go:111-118`).
- **The caller's context bounds the retry too.** `PollUntilContextTimeout` derives from `ctx`.
- **On timeout or cancellation** after a retryable error, `Apply` returns the last no-match error, wrapped as today (`failed to apply resources: ...`). The reconcile then reports `ApplyFailed` with the same message it gives today. A poll error never replaces a real apply error.
- **No new log at Info.** The single V(1) line follows the logging rules in `AGENTS.md` (capitalised message, structured keys).
- `immediate=true` keeps the first attempt synchronous, so the common path costs nothing.

### D4. Counting across attempts

**Context.** A retry re-runs the whole set. On the second attempt the CRD dry-run compares equal and reports `UnchangedAction`. A naive count would turn "2 created" into "1 created, 1 unchanged", and the `Applied N resources (...)` event and log would misreport. The existing spec asserts `result.Created == 2` (`apply_test.go:226`).

**Decision.**
- `ApplyAllStaged` returns the partial change set together with its error (`manager_apply.go:379-416`: every error return passes `changeSet`).
- `Apply` keeps a ledger keyed by `ChangeSetEntry.ObjMetadata`. It stores the first `CreatedAction` or `ConfiguredAction` seen for an object. An object only ever seen as `UnchangedAction` counts as Unchanged.
- The counts come from the ledger after the successful attempt.
- Only objects in the successful attempt's change set are counted. That attempt's change set covers every input object, so nothing is dropped and nothing is double-counted.

### D5. Deterministic reproduction instead of timing

**Context.** On this host the race did not reproduce naturally: 264 local runs of the unchanged suite, 0 failures. The recorded runs, all against envtest 1.36.0 from `bin/k8s`, are:

| Load | Runs | Failures |
| --- | --- | --- |
| serial | 30 | 0 |
| `xargs -P 10` | 30 | 0 |
| `-P 24` | 64 | 0 |
| CI seed 1791087360, `-P 16` | 100 | 0 |
| `taskset -c 0`, `-P 6` | 40 | 0 |

The loop scripts are `p3-operator-flaky-loop.sh` and `p3-operator-flaky-par.sh` in the supervisor scratchpad.

A natural "before and after" comparison therefore proves nothing on its own.

**Decision.**
- The integration suite gets a `laggingMapper`, a `meta.RESTMapper` wrapper. For one configured `GroupKind` it returns `&meta.NoKindMatchError{...}` from `RESTMapping` and `RESTMappings` until a lag window has passed since the first lookup of that kind. Every other kind delegates to the real mapper from `apiutil.NewDynamicRESTMapper(cfg, httpClient)`.
- A per-spec client built with `client.New(cfg, client.Options{Mapper: lagging})` feeds `apply.NewResourceManager`. The kstatus poller also uses `c.RESTMapper()`, but it only looks up the CRD's own kind, which is never lagged.
- Before the fix, an `Apply` of `{widget, crd}` under a 1 s lag fails with the CI error text: `no matches for kind "Widget" in version "test.example.com/v1"`.
- After the fix it succeeds with `Created == 2` and takes at least the lag.
- Section 1 lands the harness with a spec that asserts today's failure. Section 2 flips that spec to assert success when it lands the fix.

**Rationale.** The harness simulates exactly what the CI log showed: the kind is missing from client-side discovery after the CRD stage. It does so without depending on scheduler timing.

## Risks / Trade-offs

- **Up to 10 s of extra reconcile time** when a set's CRD never becomes served, for example when the CRD is Established but its version is `served: false`. Previously this failed at once. The error and reason are unchanged. The 10 s is bounded and only applies to a set that contains the defining CRD.
- **A whole-set retry repeats the dry runs of already-applied objects.** SSA is idempotent, and the dry runs are cheap compared with a reconcile requeue. The CRD stage's `WaitForSet` returns at once on the retry, because the CRD is already Established.
- **The lag mapper tests the retry, not the API server.** The real lag is covered by the existing spec (`apply_test.go:176`) staying in the suite. Section 2 runs it 30 times serially and 64 times at `-P 24` after the fix.
- **The cli may have the same race.** Its apply path waits for CRDs itself (cli archive `2026-10-03-order-instance-apply-by-weight`, design D3/D4) and was not examined here. If it uses a client RESTMapper after an Established wait, it has the same window. This is a follow-up to raise with the supervisor, not part of this change.
- **The ssa library could change `ApplyAllStaged`'s partial change set contract.** D4 relies on `manager_apply.go:379-416` returning `changeSet` with every error. A Dependabot bump of `fluxcd/pkg/ssa` that changes this would make the counts on the retry path wrong, but not the apply itself. The D4 integration assertion (`Created == 2` under lag) would catch it.

## Sections

1. **Spike: deterministic reproduction (test only).** The lagging mapper and a spec that asserts today's failure under a simulated lag. This records the reproduction. It ends green and ships nothing.
2. **Fix: bounded discovery retry in `apply.Apply`.** It adds the predicate, the loop and the ledger. The spike spec flips to success. It adds the no-retry and timeout specs and the predicate unit tests. It runs the before and after loops and records them here. The commit is `fix(apply): ...`.

Two sections, one PR (the default). The PR title is `fix(apply): wait for discovery of a CRD applied in the same set`.
