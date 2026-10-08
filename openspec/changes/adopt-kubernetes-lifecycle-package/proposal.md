## Why

The operator deletes an instance's objects with a loop of its own (`internal/apply/prune.go:98`): its own order, no propagation policy, and a finalizer rule written in each reconciler. The library has one deletion protocol for every frontend (`opm/k8s/lifecycle` in library `v1.0.0-beta.7`: `NewDeletionPlan`, `Advance`, `MayReleaseHold`), enhancement 0012:D4 binds both frontends to it, and the cli moved to it in cli#350. The ownership verdict on every delete is already in the operator (opm-operator#271). This change moves the operator's two delete paths onto the plan, so a rule added to the plan reaches the operator without a second implementation.

## What Changes

- The stale prune and the deletion cleanup, of a ModuleInstance and of a ModulePackage, delete only what `lifecycle.Advance` names. One function in `internal/apply` performs the plan's reads and deletes.
- **BREAKING** Deletes are sent with Foreground propagation (the plan's), not with the API server's default.
- **BREAKING** Objects are deleted in descending kind weight (the plan's order), not in inventory order.
- The cleanup finalizer is released only on a release verdict of `lifecycle.MayReleaseHold`.
- **BREAKING** (owner decision Q1, built for the recommended option) The cleanup finalizer is also kept until every object the cleanup deleted is gone from the cluster. While objects are terminating the reconcile requeues and reports `DeletionInProgress`; after 10 minutes it reports `DeletionBlocked` and names the objects and their finalizers.
- The forced recreate inside an apply stays outside the plan, unchanged.
- No CRD field, no flag, no RBAC change, no library bump.

SemVer class: MAJOR after GA (observable delete behaviour changes). In beta it ships as the next `1.0.0-beta.N` under a `feat!:` title.

### 1. Delete paths today (origin/main `5bad00a`)

| Path | Code | Order | Propagation | Finalizer release rule | Waits for |
| --- | --- | --- | --- | --- | --- |
| Deletion cleanup, ModuleInstance | `internal/reconcile/moduleinstance.go:1282-1350` calls `apply.Prune` at `:1316` | The order of `status.inventory.entries`; the loop does not sort (`internal/apply/prune.go:109`) | None set: the DELETE carries only the UID precondition (`prune.go:156-161`), so the API server's default applies | `apply.Prune` returned no error (`moduleinstance.go:1318`, `:1344`): every entry was deleted, absent, skipped by the verdict or a kept claim. Also at once when `spec.prune` is false or the inventory is empty (`:1292-1300`), and on the orphan annotation with a missing ServiceAccount (`:1366-1386`) | Nothing. The finalizer goes in the same reconcile, as soon as the DELETE calls are accepted (`:1344`) |
| Deletion cleanup, ModulePackage | `internal/reconcile/modulepackage.go:953-1010`, `apply.Prune` at `:983` | Same | Same | Same (`:959-968`, `:1009`) | Nothing |
| Stale prune, ModuleInstance | `moduleinstance.go:684`, `pruneStaleResources` at `:1482-1515` | The order of the stale set | Same | No finalizer involved. A failed entry fails the reconcile with `PruneFailed` (`:1503`) or stalls with `ImpersonationFailed` when forbidden (`:1499-1501`) | Nothing |
| Stale prune, ModulePackage | `modulepackage.go:787-803` | Same | Same | Same (`:790-794`) | Nothing |
| Forced recreate | `internal/apply/apply.go:98` sets Flux `Force`; every delete passes `deleteGuard.Delete` (`internal/apply/claims.go:161-177`) | Flux's apply order | Background, set by Flux (`fluxcd/pkg/ssa v0.77.0`, `manager_apply.go:144`, `:259`) | None | Not traced (Flux internal) |
| CLI-owned instance, the operator's own instance | `moduleinstance.go:1146-1155`, `:1193-1202` | n/a | n/a | Released without any delete | Nothing |

`internal/apply/callsites_test.go:194-196` keeps the list of delete sites closed: `prune.go` (1) and `claims.go` (2). Failure handling today: a failed read or delete returns an error and controller-runtime retries with backoff (`moduleinstance.go:1334-1335`); Forbidden under impersonation stalls with `ImpersonationFailed` and a 30 minute recheck (`:1319-1333`, `internal/reconcile/backoff.go:17`); a missing ServiceAccount stalls with `DeletionSAMissing` (`:1388-1402`).

### 2. After this change

Both cleanups and both stale prunes run on the library plan. The forced recreate and the two no-delete releases do not.

| | Deletion cleanup (ModuleInstance, ModulePackage) | Stale prune (ModuleInstance, ModulePackage) |
| --- | --- | --- |
| Order, today | Inventory order | Stale set order |
| Order, after | Descending kind weight, stable (`lifecycle/plan.go:59`) | Same |
| Propagation, today | API server default | API server default |
| Propagation, after | Foreground (`lifecycle/advance.go:228`) | Foreground |
| Finalizer released, today | When `apply.Prune` returns no error | n/a |
| Finalizer released, after | When `MayReleaseHold` releases and every object this cleanup deleted is gone | n/a |
| Objects still terminating, today | Not observed; the instance is gone | Not observed |
| Objects still terminating, after | The reconcile ends, `Ready=False`/`Reconciling=True` with reason `DeletionInProgress`, requeue after 10 s. It never blocks in process | Not observed and not waited for: the entry leaves the inventory as today |
| Stuck object, today | Not visible: reported as pruned, instance gone | Not visible |
| Stuck object, after | After 10 minutes: `Ready=False`/`Stalled=True`, reason `DeletionBlocked`, message names each object and its finalizers; one Warning event; recheck every minute | Not visible (unchanged; see Risks in design.md) |

The hold verdict maps onto the statuses that exist today; no reason is renamed:

| `MayReleaseHold` | Operator |
| --- | --- |
| `prune-disabled`, `inventory-empty` | Finalizer removed, as today |
| `force-orphan` (ServiceAccount missing, annotation `opm.dev/force-delete-orphan=true`) | `OrphanedOnDeletion` event, inventory cleared, finalizer removed, as today |
| `identity-unavailable`, ServiceAccount missing | Stalled `DeletionSAMissing`, as today |
| `identity-unavailable`, other impersonation error | Stalled `ImpersonationFailed`, as today |
| `cleanup-forbidden` | Stalled `ImpersonationFailed` when impersonating, else an error and a retry, as today |
| `cleanup-incomplete` | Error, finalizer kept, retry with backoff, as today |
| `cleanup-complete` | Wait for the deleted objects to be gone, then remove the finalizer |

### 3. What is kept from recent operator work

| Work | Spec | How it is kept |
| --- | --- | --- |
| `spec.dataPolicy` and kept PersistentVolumeClaims (#267) | `prune-stale-resources` "PersistentVolumeClaims are kept unless the instance opts out", `finalizer-and-deletion` "A kept claim never holds the finalizer" | A claim the policy keeps never enters the plan. It is read and judged for the report only (kept, left behind, or gone), with the same verdict. `ClaimsKept` events unchanged |
| Kept claims on a forced recreate (#268) | `ssa-apply` "A forced recreate keeps PersistentVolumeClaims unless data deletion is allowed" | Not touched: the forced recreate stays outside the plan |
| Stuck deletes and the ServiceAccount watch (#263) | `finalizer-and-deletion` "Recovery signals of a stalled deletion trigger a reconcile", `serviceaccount-impersonation` "A created ServiceAccount wakes the instances that impersonate it" | Not touched. The missing ServiceAccount becomes `HoldInput.Identity = missing`; the annotation becomes `Policy.ForceOrphan` |
| Claims through a registry blip (#262) | `registration-acceptance` | Not touched: no code of the Platform or TransformerRegistration reconcilers changes |
| Narrowed impersonation (#260) | `serviceaccount-impersonation` "Controller may impersonate ServiceAccounts only" | The plan's reads and deletes use the same impersonated client. No new verb (item 7) |
| Both identities until settled, UID precondition (#271) | `prune-stale-resources` "The prune judges with the recorded identities", "Deletes carry the UID precondition"; `finalizer-and-deletion` "Deletion cleanup judges every object with the delete verdict" | A plan takes one owner UUID. The runner builds one plan per identity, in the same order as today: entries the first plan skips as `owner-mismatch` or `adopted-elsewhere` form the next plan. The UID precondition is the plan's |

The forced recreate keeps "UID precondition only" (owner decision) and **stays where it is**, in `deleteGuard`. It does not move to the plan runner: it is part of an apply, its object stays in the inventory, the delete is sent by Flux's engine, and a Foreground delete would leave the name terminating when Flux creates the object again.

### 4. Ordering

The plan's order is the library's: descending kind weight, stable among equal weights. This change adds no ordering of its own, between or within kinds. One consequence of the two-identity rule: objects that only the second identity owns are deleted in a second plan, after the first; each plan is in the library's order.

### 5. A deletion that never finishes

There is no timeout that gives up. The operator never removes the finalizer while an object it deleted still exists.

- Up to 10 minutes: reason `DeletionInProgress`, recheck every 10 s.
- After 10 minutes (measured from the `deletionTimestamp` of the oldest remaining object): reason `DeletionBlocked`, `Stalled=True`, a Warning event, recheck every minute. The message names each remaining object and its finalizers, for example `Deployment/media/jellyfin (finalizers: foregroundDeletion)`.
- Ways out, documented on `docs/site/operating/delete-an-instance-safely.md`: (1) remove what holds the named object (for `foregroundDeletion`: a dependent that cannot terminate, for example a Pod on a dead node); (2) set `spec.prune` to false on the deleting object: the next reconcile releases the finalizer and leaves the objects as they are. The annotation `opm.dev/force-delete-orphan` keeps its one meaning (missing ServiceAccount) and does not lift this wait.
- The periodic reconcile plays no part: a deleting object requeues itself.

### 6. Breaking, and the release note

Breaking: yes, `feat!:`. PR title: `feat!: delete through the library deletion plan`. Release note for the PR body:

> The operator now deletes with Foreground propagation and in descending kind order, on a prune and on the deletion of a ModuleInstance or ModulePackage. What you notice: (1) a deleted Deployment, StatefulSet or Job stays visible, Terminating, until its Pods are gone; before, it disappeared at once and its Pods followed. (2) With `spec.prune: true`, a ModuleInstance now stays Terminating until every object it deleted is gone, so `kubectl delete moduleinstance` returns later. (3) A Pod that cannot terminate now holds the ModuleInstance: its `Ready` condition says `DeletionInProgress`, then `DeletionBlocked` after 10 minutes, and names the object. To release it, fix the named object or set `spec.prune` to false. No field, flag or RBAC rule changes; nothing to migrate.

The cli needs no change to drive this: `opm instance delete` on an operator-managed instance already waits for the ModuleInstance under `--timeout`.

### 7. RBAC

No new verb. The generated role (`config/rbac/role.yaml`) grants the manager no verb on workload kinds at all: `serviceaccounts` `get, impersonate, list, watch`, and its own kinds. Deletes are sent as the impersonated ServiceAccount, which needs `get` and `delete` on each kind, as today. Foreground is an option on the same DELETE request; the API server sets the `foregroundDeletion` finalizer itself, so no `update` on finalizers is needed. The plan sends no `deletecollection`. The wait reads objects with `get`, which the plan's read already needs. `moduleinstances/finalizers` and `modulepackages/finalizers` `update` are already granted.

### 8. API and CRD

No CRD change in the recommended build. Two owner decisions are open; the proposal is built for the recommended option of each (details in design.md, "Open Questions"):

- **Q1**: does the cleanup finalizer wait until the deleted objects are gone? Recommended: yes.
- **Q2**: is the deletion `State` stored in `status` across reconciles (a new status field), as 0012:D4 allows? Recommended: no; each reconcile starts a plan from the zero State.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `finalizer-and-deletion`: the cleanup runs the library plan; the finalizer follows the hold verdict and waits for deleted objects to be gone; a deletion that does not finish is reported.
- `prune-stale-resources`: the prune runs the library plan (order, Foreground); it does not wait.
- `kubernetes-tier-adoption`: deletes of inventory objects are sent only as the plan names them; the delete-site list.
- `status-conditions`: the reasons `DeletionInProgress` and `DeletionBlocked`.
- `events-emission`: the events of a waiting and of a blocked deletion.

## Impact

- `internal/apply`: new plan runner (the only delete site besides `claims.go`); `Prune` becomes a caller of it; `callsites_test.go`.
- `internal/reconcile/moduleinstance.go`, `modulepackage.go`: the two deletion handlers and the two prune call sites.
- `internal/status`: two reasons and their notes.
- `docs/site/operating/deletion-and-pruning.md`, `delete-an-instance-safely.md`, `docs/site/diagnostics/operator-conditions.md`.
- Tests: envtest runs no garbage collector, so a Foreground delete leaves an object terminating there; section 1 of tasks.md is a spike that proves this and adds a test helper.
- No dependency change: `opm/k8s/lifecycle` is in the pinned library `v1.0.0-beta.7` (`go.mod:15`).

Size: five sections. They do not fit one agent in about 90 turns: the two reconcilers are symmetric and their deletion tests are many. Split into two launches on the same branch and one PR: sections 1 to 3 (spike, runner, stale prune), then sections 4 and 5 (cleanup, wait, docs). Each section ends green; `main` sees the change once.
