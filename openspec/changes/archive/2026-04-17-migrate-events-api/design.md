## Context

`controller-runtime` v0.23 deprecated `manager.GetEventRecorderFor` (returns the legacy `client-go/tools/record.EventRecorder`, which speaks the core/v1 Event API) in favor of `manager.GetEventRecorder` (returns `client-go/tools/events.EventRecorder`, which speaks events.k8s.io/v1). The legacy interface is scheduled for removal. Today the controller calls the deprecated method at `cmd/main.go:242`, fails staticcheck SA1019 in CI lint, and uses the legacy `record.EventRecorder` type throughout `internal/controller/modulerelease_controller.go`, `internal/reconcile/modulerelease.go` (9 emission call sites), and ~25 test fixtures in `internal/controller/modulerelease_*_test.go`.

The two interfaces are not source-compatible:

| Aspect | Legacy (`record`) | New (`events`) |
| --- | --- | --- |
| Methods | `Event(obj, type, reason, msg)`, `Eventf(obj, type, reason, fmt, args...)`, `AnnotatedEventf(...)` | `Eventf(regarding, related runtime.Object, type, reason, action, note string, args...)` only |
| `related` object | n/a | required parameter (nil allowed) |
| `action` field | n/a | required string parameter (action verb describing what was done) |
| Fake recorder | `record.NewFakeRecorder(N)` — events flow over `chan string` formatted as `"TYPE REASON MESSAGE"` | `events.NewFakeRecorder(N)` — events flow over `chan string` formatted as `"TYPE REASON NOTE"` (action is omitted from fake output) |

## Goals / Non-Goals

**Goals:**

- Eliminate the staticcheck SA1019 lint failure on `cmd/main.go:242`.
- Move `ModuleReleaseReconciler` and the reconcile pipeline to the events.k8s.io/v1 API end-to-end, in one atomic change.
- Keep the existing event reason vocabulary (`Applied`, `ApplyFailed`, `Pruned`, `PruneFailed`, `SourceNotReady`, `RenderFailed`, `Suspended`, `Resumed`, `NoOp`, `ReconciliationSucceeded`) unchanged so consumers reading by reason continue to work.
- Choose a stable, documented `action` vocabulary so downstream automation can filter on it.

**Non-Goals:**

- Adding new event reasons or restructuring event semantics.
- Migrating other controllers (e.g. `BundleReleaseReconciler` does not use an `EventRecorder` today).
- Changing the broadcaster wiring — controller-runtime still wires the events.k8s.io broadcaster automatically when `GetEventRecorder` is called.
- Updating CRD spec/status, RBAC, or webhooks.
- Backporting the change behind a feature flag — there is no value in supporting both code paths.

## Decisions

### D1. One-shot migration over `//nolint`

Apply the full type/signature migration in one change rather than suppressing the lint with `//nolint:staticcheck`.

**Rationale:** The legacy API will be removed in a future controller-runtime release; suppression accumulates debt. The blast radius is contained to one controller, one reconcile package, and its test files — small enough to do atomically per Principle VIII when narrowed to the EventRecorder seam.

**Alternatives:** (a) `//nolint` on line 242 — defers cost, leaves type still legacy. (b) Adapter shim wrapping `events.EventRecorder` to look like `record.EventRecorder` — adds permanent indirection for no gain.

### D2. Action vocabulary

Each existing reason maps to exactly one action verb, taken from the imperative form of what the reconcile is doing at that point:

| Reason | Action | Where emitted |
| --- | --- | --- |
| `Suspended` | `Suspend` | `internal/reconcile/modulerelease.go:98` |
| `Resumed` | `Resume` | `:120` |
| `RenderFailed` / source-not-ready / similar Phase 1–4 warnings | `Render` | `:263` |
| `NoOp` | `Reconcile` | `:312` |
| `ApplyFailed` | `Apply` | `:340` |
| `Applied` | `Apply` | `:356` |
| `ReconciliationSucceeded` | `Reconcile` | `:392` |
| `PruneFailed` | `Prune` | `:564` |
| `Pruned` | `Prune` | `:573` |

**Rationale:** Action is meant to be a stable, low-cardinality verb describing the operator's action (kubernetes/enhancements KEP-383). Coupling action to the Phase rather than the outcome keeps cardinality small and makes filtering by `action=Apply` return both successes and failures of the apply phase.

**Alternatives:** Use the reason verbatim (high cardinality, redundant with `reason`); leave action empty (the broadcaster accepts it but defeats the field's purpose and may trigger validation in future apiserver versions).

### D3. `related` is `nil` for every emission

All current call sites describe an action on the `ModuleRelease` itself; there is no second related object (the rendered children are tracked via inventory, not events).

**Rationale:** Adding a related object purely to satisfy the parameter would mislead consumers. `nil` is explicitly allowed by the broadcaster.

### D4. Test fakes switch to `events.NewFakeRecorder`

Every `record.NewFakeRecorder(N)` becomes `events.NewFakeRecorder(N)`. The fake channel format is `"TYPE REASON NOTE"` — identical to the old fake (action is omitted), so existing assertion strings remain valid.

**Rationale:** Keeping the legacy fake while the production type is the new one would require an adapter and prevent the legacy import from being dropped, so the staticcheck would still trip.

### D5. No transitional period

Field type, all call sites, and all tests change in one PR. No dual-API helper is introduced.

**Rationale:** Halfway state would leave the code uncompilable or require an adapter; the migration is purely mechanical and safer in one atomic commit.

## Risks / Trade-offs

- **Test-string assertions break silently** → Greppable migration: locate every `Expect(...recorder.Events).To(Receive(...))` and `<-recorder.Events` and update the expected substring. Run `make test` after each test file is migrated. Mitigated by the fake recorder's channel still being a `chan string`, so the type system catches structural mismatches even if string contents change.
- **Action vocabulary churn** → Once shipped, changing an action string is a behavior change for anyone filtering events. Mitigation: action table in this design is the source of truth; future emission sites pick from the same vocabulary or extend it with an ADR.
- **events.k8s.io requires a slightly richer broadcaster setup** → controller-runtime handles this internally when `GetEventRecorder` is invoked; no manager wiring changes needed. Verified against `sigs.k8s.io/controller-runtime@v0.23.3/pkg/manager/internal.go`.
- **Downstream tooling reading core/v1 Events** → The events.k8s.io broadcaster bridges to core/v1 Event objects, so `kubectl get events` continues to work. Tooling that parses the (previously empty) `action` field will start seeing values; this is additive.

## Migration Plan

1. Land the type change and main.go switch first (one commit) — controller will not compile until call sites and tests follow, so include them in the same PR.
2. Update `internal/reconcile/modulerelease.go` emission sites per the action table in D2.
3. Update test fixtures in `internal/controller/modulerelease_reconcile_test.go` and `modulerelease_controller_test.go`: swap fake constructor + adjust assertions for new channel format.
4. Drop the `k8s.io/client-go/tools/record` import wherever it becomes unused (`goimports` will catch it; `golangci-lint` will fail on unused imports if not).
5. Run `make manifests generate fmt vet lint test` locally; expect SA1019 to be gone and all envtest specs green.
6. Push, confirm GitHub Actions lint job passes.

**Rollback:** Single PR; revert the merge commit. No data, CRD, or RBAC migration to undo.

## Open Questions

- None blocking. If a future emission site genuinely needs a `related` object (e.g. an event tied to a specific child resource), extend D3 case-by-case rather than retrofitting.
