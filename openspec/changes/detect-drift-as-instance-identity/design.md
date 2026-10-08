## Context

`ReconcileModuleInstance` (`internal/reconcile/moduleinstance.go`) runs drift detection in Phase 4 with `params.ResourceManager`, which wraps the manager's own client. The impersonated client is built later, by `buildApplyClient`, only when the reconcile is not a `NoOp`; the `NoOp` branch builds a second one for the health reads (`judgeInstanceHealth`). `apply.DetectDrift` takes the resource manager as an argument, so the identity is decided by the caller alone.

`buildApplyClient` already resolves the identity in the order the task asks for: `spec.serviceAccountName`, else `--default-service-account`, else the operator's own client and resource manager.

## Goals / Non-Goals

**Goals:**

- The dry-run of drift detection is sent by the same client as the apply of the same reconcile.
- A refused dry-run and an identity that cannot be built are stated on the `Drifted` condition.
- No fallback to the operator's identity.

**Non-Goals:**

- No change to how `Ready` reacts to a missing ServiceAccount on a `NoOp` reconcile.
- No change to RBAC files, to the ServiceAccount watch, to the restore rules or to ModulePackage.
- No removal of rules from the operator's ClusterRole that only the old drift detection used.

## Decisions

### Build the apply client once, before drift detection

The call to `buildApplyClient` moves from after the `NoOp` return to before drift detection. Its three results (resource manager, client, error) serve drift detection, the `NoOp` health reads, the apply, the prune and the post-apply health reads.

```go
applyRM, applyClient, impErr := buildApplyClient(ctx, params, &mi)
phases.driftRan = true
missing, phases.driftFailed = detectDrift(ctx, applyRM, impErr, &mi, applyList)
...
if isNoOp {
    v := judgeNoOpHealth(ctx, params, &mi, applyClient, impErr)   // reuses the client
    ...
}
if impErr != nil { /* stall with ImpersonationFailed, as today */ }
```

Alternative considered: give `DetectDrift` the rest config and let it build a client. Rejected: two places would resolve the identity, and they could disagree.

The impersonation error is kept and acted on in two places. Drift detection reports it and goes on, because a reconcile with unchanged digests has nothing to apply and MUST stay a `NoOp`. The apply phase stalls on it, unchanged.

### A refused dry-run is `Drifted=Unknown`, reason `DriftCheckForbidden`

`fluxssa.ResourceManager.Diff` wraps the API error in a `DryRunErr` that unwraps to the `StatusError`, so the existing `isForbidden` helper classifies it. The message is the API server's: it names the user, the verb and the resource, which is what the tenant needs to fix the Role.

`Unknown` is the honest value: the operator did not learn whether the object drifted. Leaving the condition absent would read as "no drift"; `True` would claim a difference nobody saw. The condition is removed by the next drift detection that succeeds and finds nothing, replaced by `True` when it finds drift, and removed by a successful apply of the rendered set, as today.

The reason applies whatever the identity is, the operator's own included: a refused dry-run is the same fact in both cases, and the message names who was refused.

Alternative considered: stall the instance (`Ready=False`, `ImpersonationFailed`) as a forbidden apply does. Rejected: drift detection is informational (ADR-012) and MUST NOT move `Ready`; an instance whose objects are applied and healthy stays `Ready`.

### An identity that cannot be built is `Drifted=Unknown`, reason `ImpersonationFailed`

The existing reason is reused: it is the same cause the apply phase reports on `Ready`. The message starts with "drift detection did not run:".

### Other dry-run failures are unchanged

A dry-run that fails for another reason (a timeout, a server error) keeps today's behaviour: the counter moves and the `Drifted` condition is left as it was. The task names the refusal only.

## Reconcile phase impact

- Source, Render: none.
- Plan (Phase 4): the apply client is built here; drift detection uses it.
- Apply, Prune: reuse the client built in Phase 4; behaviour unchanged.
- Status: `Drifted` gains the status `Unknown` with two reasons. The deferred commits already own the `Drifted` condition.

## Risks / Trade-offs

- A ServiceAccount that may `patch` but not `get` an object: `Diff` ignores the failed read and compares the dry-run result with an empty object, so drift is reported for an object that did not drift. The apply path has the same blind read. Mitigation: none in this change; the tenancy docs ask for `get` beside `patch`. Recorded as a follow-up.
- An instance whose ServiceAccount was deleted while its digests are unchanged stays `Ready=True` and reads `Drifted=Unknown`. That is today's `Ready` behaviour; whether such an instance stalls is an owner decision and not taken here.
- The dry-run of the first failing object ends the detection, as today: one forbidden object hides the drift of the others for that reconcile.

## Research & Decisions

### Does the operator's ClusterRole need a new rule

**Context**: The task forbids a new rule.
**Explored**: `config/rbac/role.yaml` and `test/integration/reconcile/impersonation_rbac_test.go`: the role holds `get`, `impersonate`, `list`, `watch` on `serviceaccounts`. The dry-run is a `PATCH` with `dryRun=All` sent as the ServiceAccount, authorised by the ServiceAccount's bindings.
**Decision**: No rule is added.
**Rationale**: Impersonation is the only right the operator itself uses for the call.

### How a test sees the impersonation header

**Context**: The task asks for a test that proves the header.
**Explored**: `rest.HTTPWrappersForConfig` applies `Config.WrapTransport` inside the impersonating round tripper, so a transport wrapped onto the manager's rest config sees `Impersonate-User` on every request of the client built from it.
**Decision**: The envtest specs wrap the rest config with a recording round tripper and assert on the dry-run `PATCH`. A unit test in `internal/apply` proves the same against an `httptest` server without envtest.
**Rationale**: The header is the fact; a refused request alone would prove only the effect.
