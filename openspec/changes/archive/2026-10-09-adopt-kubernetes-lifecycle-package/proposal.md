## Why

The operator deletes an instance's objects with a loop of its own (`internal/apply/prune.go:98`): its own order, no propagation policy, and a finalizer rule written in each reconciler. The library has one deletion protocol for every frontend (`opm/k8s/lifecycle` in library `v1.0.0-beta.8`: `NewDeletionPlan`, `Advance`, `MayReleaseHold`), enhancement 0012:D4 binds both frontends to it, and the cli moved to it in cli#350. The ownership verdict on every delete is already in the operator (opm-operator#271). This change moves the operator's two delete paths onto the plan, so a rule added to the plan reaches the operator without a second implementation.

## What Changes

- The stale prune and the deletion cleanup, of a ModuleInstance and of a ModulePackage, delete only what `lifecycle.Advance` names. One function in `internal/apply` performs the plan's reads and deletes.
- **BREAKING** Deletes are sent with Foreground propagation (the plan's), not with the API server's default.
- **BREAKING** Objects are deleted in descending kind weight (the plan's order), not in inventory order.
- The cleanup finalizer is released only on a release verdict of `lifecycle.MayReleaseHold`.
- **BREAKING** (owner decision of 2026-10-09) The cleanup finalizer is also kept until every object the cleanup deleted is gone from the cluster. While objects are terminating the reconcile requeues and reports `DeletionInProgress`; after 10 minutes it reports `DeletionBlocked` and names the objects and their finalizers.
- (owner decision of 2026-10-09) Once the cleanup sent every delete, the loss of the ServiceAccount or of its rights no longer stalls the deletion: the finalizer is released with a `DeletionUnconfirmed` event. The `Ready` reason is the record that every delete was sent; no CRD field is added.
- **BREAKING** An inventory that holds only kept PersistentVolumeClaims is released without a read, also when the ServiceAccount is missing. Today that deletion stalls with `DeletionSAMissing`.
- The forced recreate inside an apply stays outside the plan, unchanged.
- No CRD field, no flag, no RBAC change, no library bump.

SemVer class: MAJOR after GA (observable delete behaviour changes). In beta it ships as the next `1.0.0-beta.N` under a `feat!:` title.

### 1. Delete paths today (origin/main `a418b40`; written on `5bad00a`, line numbers corrected after the merge of opm-operator#273 and #274)

| Path | Code | Order | Propagation | Finalizer release rule | Waits for |
| --- | --- | --- | --- | --- | --- |
| Deletion cleanup, ModuleInstance | `internal/reconcile/moduleinstance.go:1389-1457` calls `apply.Prune` at `:1423` | The order of `status.inventory.entries`; the loop does not sort (`internal/apply/prune.go:109`) | None set: the DELETE carries only the UID precondition (`prune.go:156-161`), so the API server's default applies | `apply.Prune` returned no error (`moduleinstance.go:1425`, `:1451`): every entry was deleted, absent, skipped by the verdict or a kept claim. Also at once when `spec.prune` is false or the inventory is empty (`:1399-1407`), and on the orphan annotation with a missing ServiceAccount (`:1473-1493`) | Nothing. The finalizer goes in the same reconcile, as soon as the DELETE calls are accepted (`:1451`) |
| Deletion cleanup, ModulePackage | `internal/reconcile/modulepackage.go:1061-1122`, `apply.Prune` at `:1091` | Same | Same | Same (`:1067-1076`, `:1117`) | Nothing |
| Stale prune, ModuleInstance | `moduleinstance.go:744`, `pruneStaleResources` at `:1589-1622` | The order of the stale set | Same | No finalizer involved. A failed entry fails the reconcile with `PruneFailed` (`:1610`) or stalls with `ImpersonationFailed` when forbidden (`:1606-1608`) | Nothing |
| Stale prune, ModulePackage | `modulepackage.go:816-832` | Same | Same | No finalizer involved. Every prune error is `PruneFailed` and transient (`:819-824`); there is no Forbidden branch | Nothing |
| Forced recreate | `internal/apply/apply.go:113` sets Flux `Force`; every delete passes `deleteGuard.Delete` (`internal/apply/claims.go:164-183`) | Flux's apply order | Background, set by Flux (`fluxcd/pkg/ssa v0.77.0`, `manager_apply.go:144`, `:259`) | None | Not traced (Flux internal) |
| CLI-owned instance, the operator's own instance | `moduleinstance.go:1253-1262`, `:1300-1309` | n/a | n/a | Released without any delete | Nothing |

`internal/apply/callsites_test.go:242-245` keeps the list of delete sites closed: `prune.go` (1) and `claims.go` (2). Failure handling today: a failed read or delete returns an error and controller-runtime retries with backoff (`moduleinstance.go:1441-1442`); Forbidden under impersonation stalls with `ImpersonationFailed` and a 30 minute recheck (`:1426-1440`, `internal/reconcile/backoff.go:17`); a missing ServiceAccount stalls with `DeletionSAMissing` (`:1495-1509`).

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
| Objects still terminating, after | The reconcile ends, `Ready=False`/`Reconciling=True` with reason `DeletionInProgress`, requeue after 1 s to 60 s (a quarter of the age of the oldest terminating object). It never blocks in process | Not observed and not waited for: the entry leaves the inventory as today |
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
| `inventory-empty` because only kept claims are left, identity missing or failed | **New**: finalizer removed without a read, `DeletionUnconfirmed` event. Today: stalled `DeletionSAMissing` (`moduleinstance.go:1399`, `:1410-1415`) |
| `cleanup-complete` | Record it (`Ready` reason `DeletionInProgress`), wait for the deleted objects to be gone, then remove the finalizer |
| At a recheck, after `cleanup-complete` was recorded: the ServiceAccount is gone (the controller's read of it is answered NotFound) | **New**: finalizer removed, `DeletionUnconfirmed` event (owner decision of 2026-10-09: "Only when the ServiceAccount is gone") |
| At a recheck, after `cleanup-complete` was recorded: any other answer (a 403, a 401, a 5xx, a throttle, a timeout), also when the ServiceAccount lost its rights and stays | Finalizer and wait reason kept, the message says why, retried with backoff, `DeletionBlocked` after 10 minutes; way out `spec.prune=false` |

### 3. What is kept from recent operator work

| Work | Spec | How it is kept |
| --- | --- | --- |
| `spec.dataPolicy` and kept PersistentVolumeClaims (#267) | `prune-stale-resources` "PersistentVolumeClaims are kept unless the instance opts out", `finalizer-and-deletion` "A kept claim never holds the finalizer" | A claim the policy keeps never enters the plan. It is read and judged for the report only (kept, left behind, or gone), with the same verdict. `ClaimsKept` events unchanged |
| Kept claims on a forced recreate (#268) | `ssa-apply` "A forced recreate keeps PersistentVolumeClaims unless data deletion is allowed" | Not touched: the forced recreate stays outside the plan |
| Stuck deletes and the ServiceAccount watch (#263) | `finalizer-and-deletion` "Recovery signals of a stalled deletion trigger a reconcile", `serviceaccount-impersonation` "A created ServiceAccount wakes the instances that impersonate it" | The two triggers are not touched. The missing ServiceAccount becomes `HoldInput.Identity = missing`; the annotation becomes `Policy.ForceOrphan`. The `DeletionSAMissing` stall keeps its rule when the identity is lost before every delete was sent; two cases no longer enter it (item 2) |
| Claims through a registry blip (#262) | `registration-acceptance` | Not touched: no code of the Platform or TransformerRegistration reconcilers changes |
| Narrowed impersonation (#260) | `serviceaccount-impersonation` "Controller may impersonate ServiceAccounts only" | The plan's reads and deletes use the same impersonated client. No new verb (item 7) |
| Both identities until settled, UID precondition (#271) | `prune-stale-resources` "The prune judges with the recorded identities", "Deletes carry the UID precondition"; `finalizer-and-deletion` "Deletion cleanup judges every object with the delete verdict" | A plan takes one owner UUID. The runner builds one plan per identity, in the same order as today: entries the first plan skips as `owner-mismatch` or `adopted-elsewhere` form the next plan. The UID precondition is the plan's |

The forced recreate keeps "UID precondition only" (owner decision) and **stays where it is**, in `deleteGuard`. It does not move to the plan runner: it is part of an apply, its object stays in the inventory, the delete is sent by Flux's engine, and a Foreground delete would leave the name terminating when Flux creates the object again.

### 4. Ordering

The plan's order is the library's: descending kind weight, stable among equal weights. This change adds no ordering of its own, between or within kinds. One consequence of the two-identity rule: objects that only the second identity owns are deleted in a second plan, after the first; each plan is in the library's order.

### 5. A deletion that never finishes

There is no timeout that gives up. The operator never removes the finalizer while an object it deleted, and can still read, exists.

- Up to 10 minutes: reason `DeletionInProgress`. The recheck interval is a quarter of the age of the oldest terminating object, at least 1 s and at most 60 s.
- After 10 minutes (measured from the `deletionTimestamp` of the oldest remaining object, by the controller's clock): reason `DeletionBlocked`, `Stalled=True`, a Warning event, recheck every 60 s. The message names each remaining object and the finalizer that holds it, for example `Deployment/media/jellyfin (waits for its dependents)` or `ConfigMap/media/x (finalizers: example.com/hold)`.
- Ways out, written as visible text on `docs/site/operating/delete-an-instance-safely.md` (tasks.md 6.1 carries the text): (1) remove what holds the named object; (2) set `spec.prune` to false on the deleting object: the next reconcile releases the finalizer and leaves the objects as they are. The annotation `opm.dev/force-delete-orphan` keeps its one meaning (missing ServiceAccount) and does not lift this wait.
- The ServiceAccount is deleted during the wait: the operator releases the finalizer and emits `DeletionUnconfirmed`. When only its rights disappear (the RoleBinding is deleted, the ServiceAccount stays), the deletion holds, says why, and is `DeletionBlocked` after 10 minutes; `spec.prune=false` releases it (owner decision of 2026-10-09, "Only when the ServiceAccount is gone"). Before every delete was sent, a lost identity still stalls with `DeletionSAMissing` or `ImpersonationFailed`, as today.
- The periodic reconcile plays no part: a deleting object requeues itself.

### 6. Breaking, and the release note

Breaking: yes, `feat!:`. PR title: `feat!: delete through the library deletion plan`. Release note for the PR body:

> The operator now deletes with Foreground propagation and in descending kind order, on a prune and on the deletion of a ModuleInstance or ModulePackage. What you notice:
>
> 1. A deleted object that has dependents (a Deployment and its Pods) stays visible, Terminating, until the dependents are gone.
> 2. With `spec.prune: true`, a ModuleInstance or ModulePackage stays Terminating until every object it deleted is gone, so `kubectl delete moduleinstance` returns later. Its `Ready` condition says `DeletionInProgress`.
> 3. An object that cannot terminate now holds the instance. After 10 minutes `Ready` says `DeletionBlocked` and names the object. To release it, fix the named object or set `spec.prune` to false.
> 4. If the ServiceAccount is deleted while the instance only waits, the operator lets the instance go and emits a `DeletionUnconfirmed` event. If only its rights are removed (the RoleBinding is deleted and the ServiceAccount stays), the instance keeps waiting, says why, and after 10 minutes says `DeletionBlocked`: set `spec.prune` to false to let it go. If the ServiceAccount is gone before any delete is sent, the deletion still stalls with `DeletionSAMissing`: delete the instance first and its ServiceAccount after it.
> 5. An instance whose inventory holds only PersistentVolumeClaims that `spec.dataPolicy` keeps is now deleted even when its ServiceAccount is missing; before, it stalled with `DeletionSAMissing`. The claims are left in place, as before.
>
> No field, flag or RBAC rule changes; nothing to migrate.

The first draft of this note said that a deleted Deployment, StatefulSet or Job "disappeared at once and its Pods followed" before this change. That sentence is removed: today's DELETE sets no policy, so each kind's own default applies (`k8s.io/apiserver v0.36.4`, `pkg/registry/generic/registry/store.go:891-896`), and the per-kind defaults live in `k8s.io/kubernetes`, which is not in the module cache. If a kind's default is to orphan (the reviewer recalls this for `batch/v1` Job), this change newly deletes its dependents. tasks.md 6.3 adds an e2e spec for a Job, and the note gains a sentence only if that spec shows a difference.

The cli needs no change to drive this: `opm instance delete` on an operator-managed instance already waits for the ModuleInstance under `--timeout`.

### 7. RBAC

No new verb. The generated role (`config/rbac/role.yaml`) grants the manager no verb on workload kinds at all: `serviceaccounts` `get, impersonate, list, watch`, and its own kinds. Deletes are sent as the impersonated ServiceAccount, which needs `get` and `delete` on each kind, as today. Foreground is an option on the same DELETE request; the API server sets the `foregroundDeletion` finalizer itself, so no `update` on finalizers is needed. The plan sends no `deletecollection`. The wait reads objects with `get`, which the plan's read already needs. `moduleinstances/finalizers` and `modulepackages/finalizers` `update` are already granted.

### 8. API and CRD

No CRD change. Decided on 2026-10-09:

- The finalizer waits until deleted objects are gone (owner: "Wait until gone").
- The deletion `State` is not stored (supervisor ruling). The one fact that must survive a reconcile, "every delete was sent", is the `Ready` reason `DeletionInProgress` or `DeletionBlocked` (owner: "Release the finalizer", "no new CRD field"). The two reasons and the event reason `DeletionUnconfirmed` are additions to the status vocabulary, not schema changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `finalizer-and-deletion`: the cleanup runs the library plan; the finalizer follows the hold verdict and waits for deleted objects to be gone; the wait survives the loss of the identity; a deletion that does not finish is reported; five existing requirements are reworded so that none says the finalizer goes in the reconcile that sends the deletes.
- `prune-stale-resources`: the prune runs the library plan (order, Foreground); it does not wait.
- `kubernetes-tier-adoption`: deletes of inventory objects are sent only as the plan names them; the delete-site list.
- `serviceaccount-impersonation`: the two cases that no longer stall with `DeletionSAMissing`.
- `modulepackage-reconcile-loop`: a ModulePackage deletes and waits as a ModuleInstance does; two scenarios reworded.
- `status-conditions`: the reasons `DeletionInProgress` and `DeletionBlocked`, the event reason `DeletionUnconfirmed`, the message bounds.
- `events-emission`: the events of a waiting, a blocked and an unconfirmed deletion; `ClaimsKept` and `LeftBehind` once per deletion.

## Impact

- `internal/apply`: new plan runner (the only delete site besides `claims.go`); `Prune` becomes a caller of it; `callsites_test.go`.
- `internal/reconcile/moduleinstance.go`, `modulepackage.go`: the two deletion handlers and the two prune call sites.
- `internal/status`: two reasons and their notes.
- `docs/site/operating/deletion-and-pruning.md`, `delete-an-instance-safely.md`, `docs/site/diagnostics/operator-conditions.md`.
- Tests: envtest runs no garbage collector, so a Foreground delete leaves an object terminating there; section 1 of tasks.md is a spike that proves this and adds a helper that runs for the whole of each envtest suite (`internal/controller`, `test/integration/apply`, `test/integration/reconcile`, `test/integration/operatormodule`). `test/e2e` gains specs that the CI job `test-e2e` runs on the pull request (`.github/workflows/test-e2e.yml:3-6`, `:107`); no agent uses a cluster.
- No dependency change: `opm/k8s/lifecycle` is in the pinned library `v1.0.0-beta.8` (`go.mod:15`).

Size: six sections. They do not fit one agent in about 90 turns: the two reconcilers are symmetric and their deletion tests are many. Two implement launches on the same branch, one PR, nothing merged in between:

- Launch one: sections 1 to 3 (spike and collector helper, runner, stale prune on the plan). It ends after the commit of section 3. At that point `task dev:fmt dev:vet dev:lint dev:test` passes and the proposal's gate for a pause holds (checked boxes, clean tree), but the branch is **not mergeable**: every delete is already Foreground while the cleanup still releases the finalizer at "delete accepted".
- Launch two: sections 4 to 6 (hold verdict, the wait and its record, docs and e2e specs). The PR is opened after section 6.
