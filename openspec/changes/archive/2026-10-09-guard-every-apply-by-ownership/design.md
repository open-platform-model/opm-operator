## Context

See `proposal.md` for the motivation. This change builds on `adopt-kubernetes-ownership-package` (the delete half, opm-operator#271, archived), which recorded the starting decisions of this half in its `design.md` ("The apply half: decisions recorded here"). The state of the apply side at `main` 5bad00a, library v1.0.0-beta.7 (`go.mod:15`):

Every place that writes, and what guards it:

| # | Write | Where | Guard today |
| --- | --- | --- | --- |
| 1 | Staged server-side apply of the apply list, with forced field ownership | `rm.ApplyAllStaged`, `internal/apply/apply.go:110`; from `applyInstance` (`internal/reconcile/moduleinstance.go:1559`, called at `:621`) and `applyAndPruneModulePackage` (`internal/reconcile/modulepackage.go:764`) | No ownership check. Before it: the registration shrink rule withholds a claim (`moduleinstance.go:532`), a third identity is refused (`moduleinstance.go:516`), a changed identity is stored (`moduleinstance.go:1555`, `modulepackage.go:755`). |
| 2 | Namespaces, CustomResourceDefinitions and cluster roles | The first stage of the same call (`apply.go:58-62`). The operator creates no namespace on its own: no other call writes one. | As row 1. |
| 3 | Restore of missing objects on unchanged digests (ModuleInstance only) | `planRestore`, `moduleinstance.go:587` and `:984`; applied through row 1 | As row 1. The list holds only objects the dry-run would create. |
| 4 | Forced recreate (`spec.rollout.forceConflicts`): Flux deletes and creates an object whose update is refused | Inside row 1, `apply.go:98`; the delete passes `deleteGuard.Delete`, `internal/apply/claims.go:161-177` | The claim rule (`apply.go:103-106`, `claims.go:70-97`, `:162`) and the UID precondition (`claims.go:172`). No ownership question, by owner decision of 2026-10-08. |
| 5 | Dry runs: drift (`internal/apply/drift.go:67`) and the claim check (`claims.go:85`) | | They write nothing. Drift reads each object first and fails on Forbidden (`drift.go:63`, `:102-109`). |
| 6 | Deletes of a prune and a deletion cleanup | `internal/apply/prune.go:161` | `ownership.CanDelete` and the UID precondition (the delete half). |
| 7 | Status and finalizer patches of the operator's own kinds | `moduleinstance.go:136`, `:195`, `:387`, `:788`, `:1162`, `:1223`, `:1254`, `:1450`, `:1468`, `:1475`; `modulepackage.go:425`, `:923`, `:940`, `:946`, `:1081`; `internal/controller/platform_controller.go:490`, `transformerregistration_controller.go:659`, `transformerregistration_dependents.go:163` | Not instance objects. Out of the guard, and named in the closed list of this change. |

So one call applies, and nothing asks who holds the object.

Constraints:

- `ownership.CanApply` is pure (`opm/k8s/ownership/apply.go:94`). The caller reads the live object and hands it in. It takes one identity. The library words every refusal.
- The contract is 0012:D8:R1 to R5 and R8. The operator MUST never set `Admit` (0012:D8:R6 is for `opm operator install` only).
- What #260 to #271 delivered MUST NOT change: narrowed impersonation, the periodic reconcile and the restore, drift as the identity that applies, held claims, `spec.dataPolicy`, the two recorded identities, the delete verdict.
- The deletion protocol (finalizers, order, propagation) belongs to another change and is not specified here.

Reversibility: costly two-way for the code. No stored state depends on it, so a revert restores the old behaviour. What users see (which applies are refused, the adopt annotation) is the contract of 0012:D8, which the owner accepted; this change does not decide it. Two new reason strings (`ApplyRefused`, `AdoptedElsewhere`) are status vocabulary that stays once released.

## Goals / Non-Goals

**Goals:**

- One function, the library's, decides ownership for every object the operator applies on behalf of an instance. A test keeps the list of write call sites closed.
- A refused reconcile has written nothing.
- An object adopted by another instance leaves the inventory and stays in the cluster, and the instance stays `Ready=True`.
- No extra read for a ModuleInstance.

**Non-Goals:**

- The delete side, the deletion protocol, `opm/k8s/lifecycle`.
- An ownership question inside the forced recreate. The guard has judged every object of the apply list before Flux can recreate one.
- A way for the operator to set, rewrite or remove the adopt annotation.
- A status field, condition or metric for let-go objects.
- RBAC, and which identity the operator impersonates.
- The `Admit` input.

## Decisions

### Verdict and action per case

"Inventory" is `status.inventory` as read at the start of the reconcile, matched by group, kind, namespace and name. "Ours" is the instance's identity: the render's. A is the earlier identity while a change is not settled (`status.previousInstanceUUID`, or the recorded identity when this render starts the change). The guard never asks with A: for the verdict, A is another instance's identity. Evidence for each verdict is the named test in `opm/k8s/ownership/apply_test.go` at v1.0.0-beta.7 and the branch in `apply.go`.

| # | Live object | In inventory | Library verdict | Operator action |
| --- | --- | --- | --- | --- |
| 1 | None | either | allowed (`apply.go:95-97`; "a new object is applied") | Apply (create). |
| 2 | OPM-managed, UUID label ours | either | allowed ("an inventoried object of this instance is applied", "an OPM object of this instance outside the inventory is applied") | Apply. Outside the inventory it is taken in: see "Taking an object in". |
| 3 | OPM-managed, UUID label A, change not settled | yes | allowed with ours (a UUID label alone never refuses in the inventory, `apply.go:139-146`) | Apply; the apply relabels it. |
| 4 | OPM-managed, UUID label A, change not settled | no | `other-instance` (as row 6) | Refuse the reconcile. The object may be the instance's own (a kept claim, an object a failed apply created), and it may be another record's that renders identity A (Security). The operator cannot tell them apart, so it writes nothing. The way out is the adopt annotation, which the message prints. |
| 5 | OPM-managed, UUID label of another instance | yes | allowed (as row 3; the object was shared before the upgrade, or is ours under an identity that is no longer recorded) | Apply. The apply relabels it. Accepted: the contract judges an inventoried object by its annotation only. |
| 6 | OPM-managed, UUID label of another instance | no | `other-instance` (`apply.go:118-121`; "another instance's object outside the inventory is refused") | Refuse the reconcile. |
| 7 | Not OPM-managed (no managed-by label, or another manager's value) | no | `foreign-object` (`apply.go:109-112`; "a foreign object outside the inventory is refused", "an object with no managed-by label is foreign") | Refuse the reconcile. |
| 8 | Not OPM-managed | yes | allowed ("an inventoried foreign object is applied") | Apply. The apply sets the labels again. |
| 9 | OPM-managed, no UUID label | either | allowed ("an OPM object without a UUID label is applied") | Apply. |
| 10 | Annotated `opmodel.dev/adopt` = ours | either | allowed (`apply.go:103-105`; "adoption lifts a foreign refusal", "adoption lifts an other-instance refusal", "an annotation naming this instance takes an inventoried object back") | Apply and record. This is the adopt. |
| 11 | Annotated = A, change not settled or settled | either | `adopted-elsewhere` in the inventory (as row 12); outside it as rows 13 and 14 | Let go at once, or refuse, as the row that applies says. See "The annotation that names the earlier identity". |
| 12 | Annotated for another instance | yes | `adopted-elsewhere` (`apply.go:139-146`; "an inventoried object annotated for another instance is refused") | Let go. |
| 13 | Annotated for another instance, OPM-managed, UUID label ours, the annotated one or none | no | `adopted-elsewhere` (`apply.go:114-117`, `:122-125`; "a dropped object is not taken back on the next apply", "a handed-over object stays adopted-elsewhere after the adopter applies") | Let go (it is already out of the inventory). |
| 14 | Annotated for another instance, not OPM-managed, or UUID label of a third instance | no | `foreign-object` or `other-instance` ("an annotation naming another instance lifts nothing", "... does not lift other-instance") | Refuse the reconcile. |
| 15 | Being deleted | either, any label or annotation | `terminating` (`apply.go:99-101`; five tests) | Refuse a reconcile that would write the object, and any reconcile when the object exists outside the inventory (an object this instance let go, while its adopter deletes it, refuses until it is gone). |
| 16 | Read fails, not "not found" | | no verdict | Fail safe: see "A failed read". |

What the two outcomes mean:

**Refuse the reconcile.** Nothing is written: no identity is stored, no object is applied, nothing is pruned. `Ready=False` with reason `ApplyRefused`; `Stalled` is not set; one `Warning` event `ApplyRefused` with action `Apply`. The condition message and the event carry the count and the library's message for each refused object, unchanged (at most ten, fewer past 1024 characters, then the number of the rest: the shape of `LeftBehind`). The attempt counts as a failed apply and is retried on the bounded backoff. `status.inventory`, the applied digests and both identity fields keep their values, so the next reconcile judges again. The refused object is untouched.

**Let go.** The object is left out of the apply list, of drift detection and of the restore. Its entry is left out of the inventory this reconcile records. The stale set is computed from the full render, so the prune never sees it; if it did, the delete verdict would skip it (`adopted-elsewhere`), which is the second lock. The other objects are applied and the reconcile can end `Ready=True`. The message of `Ready=True` states how many rendered objects are adopted by another instance and not applied, on every reconcile that renders, so the state is in the object's status for as long as the module renders such an object. One `Warning` event `AdoptedElsewhere`, action `Apply`, with the count and the library's messages, is emitted when that count changes and is not zero: when an object is first let go, and when one more is.

### Where the guard runs

```go
// internal/apply

// GuardInput is what one reconcile hands to the apply guard.
type GuardInput struct {
    // Resources is the apply list: the rendered set without withheld objects.
    Resources []*unstructured.Unstructured
    // Inventory is status.inventory as read at the start of the reconcile.
    Inventory []releasesv1alpha1.InventoryEntry
    // Identity is the instance's identity: the render's, or the recorded one
    // when the render carries none. Never the earlier identity of an
    // unsettled change, never empty: Guard returns an error for an empty
    // identity.
    Identity string
}

// Judged is one object the verdict did not allow, with the library's reason
// and message.
type Judged struct {
    Object  *unstructured.Unstructured
    Refuse  ownership.ApplyRefusal
    Message string
    // InInventory is what the guard handed to the verdict.
    InInventory bool
}

type GuardResult struct {
    Allowed []*unstructured.Unstructured // in the order of Resources
    TakenIn []*unstructured.Unstructured // allowed, exist, not in the inventory
    LetGo   []Judged                     // adopted-elsewhere
    Refused []Judged                     // terminating, foreign-object, other-instance
}

// Guard reads every object of in.Resources through c and asks
// ownership.CanApply. It writes nothing. A read error other than NotFound is
// returned as a *GuardReadError that names the object.
func Guard(ctx context.Context, c client.Reader, in GuardInput) (*GuardResult, error)
```

ModuleInstance flow after the change (`moduleinstance.go:484-712` today):

```text
render -> plan identities (refuse a third) -> withhold refused registrations
  -> build the client that applies (a client that cannot be built: as today for a ModuleInstance)
  -> Guard(apply list, inventory, identity)            one GET per object, as the identity that applies
  -> drift detection over Allowed minus TakenIn        reuses the guard's read result; no GET of its own
  -> plan: no-op, restore (missing + TakenIn), or apply (changed digests, or an unsettled identity change)
  -> refuse when Refused holds an object of the write list, or one that exists outside the inventory
  -> store a changed identity -> Apply(write list) -> prune(stale set of the full render)
  -> commit: inventory = rendered - expired Jobs - LetGo
```

ModulePackage flow: the same guard, before the no-op decision of `modulepackage.go:364`, which means the client that applies is built before that decision and no longer inside `applyAndPruneModulePackage` (`modulepackage.go:747`). A ModulePackage has no drift detection and no restore, so `TakenIn` and a let-go object not yet out of the inventory make the reconcile an apply of `Allowed`. A ModulePackage has no `Drifted` signal to carry a guard that could not run, so a reconcile that renders and cannot build the client that applies, or cannot read an object, fails also when its digests match: `Stalled` `ImpersonationFailed` for a missing ServiceAccount or a Forbidden read under one, `Ready=False` `ApplyFailed` on the backoff otherwise. Nothing is written and the inventory stays. Today such a package with matching digests is a silent no-op.

Reconcile phase impact: Source and Render: none. Apply: the guard before the first write; the write list excludes let-go objects. Prune: none (the stale set and the delete verdict are the delete half's). Status: the reason `ApplyRefused`; an inventory without let-go objects, also on a no-op; the count of let-go objects in the message of `Ready=True`. Events: two reasons.

### Research & Decisions

#### A refused object refuses the whole reconcile

**Context**: 0012:D8:R1 needs the verdict on every apply. The brief asks whether a refused object stops the reconcile or is skipped alone.
**Options considered**:
1. Status quo: no check. Contradicts 0012:D8:R1.
2. Skip the refused object and apply the rest. The instance then runs with a part of its render missing: a Deployment without the ConfigMap it mounts, a workload without its Namespace. `Ready` would have to say both "applied" and "not all of it".
3. Refuse the whole reconcile before the first write.
**Decision**: Option 3, for `terminating`, `foreign-object` and `other-instance`. `adopted-elsewhere` alone skips one object, because 0012:D8:R8 says so and because a refusal there would never end (the alternative the enhancement rejected).
**Rationale**: It is the fail-safe form and the cli's ("checks before any write"). A refusal that wrote nothing leaves the cluster as it was, so the retry starts from the same state. Trade-off named: availability of the instance's other objects is given up for a cluster that never holds half of a render.

#### What "would write" means on a reconcile with unchanged digests

**Context**: The operator, unlike the cli, reconciles without a change: a reconcile with unchanged digests writes nothing, or restores only what is missing (`moduleinstance.go:587`). The archived design let such a reconcile act on the let-go answer only.
**Options considered**:
1. Every refusal refuses every reconcile that renders. An inventoried object that a user deletes with foreground propagation then flips `Ready` to `False` for the seconds it is terminating, with nothing to write over it.
2. Act on `adopted-elsewhere` only when the digests are unchanged. An object the instance renders, that exists outside its inventory and belongs to another instance, then leaves the instance `Ready=True` until its render next changes, and is reported as drift that never clears.
3. Refuse when the verdict refuses an object the reconcile would write, or an object that exists and is not in the inventory.
**Decision**: Option 3.
**Rationale**: An inventoried object can only be refused as `terminating` (rows 3, 5, 8), and a reconcile that writes nothing has nothing to refuse there; health and the restore report and repair it. An object that exists outside the inventory is one the instance does not hold but renders: that is the state 0012:D8:R1 refuses, whether or not the digests moved.

#### Taking an object in

**Context**: An allowed object that exists and is not in the inventory (rows 2, 9, 10 outside the inventory) must be applied and recorded: an adopted object, an object taken back after a hand-over, a kept claim the render names again. With unchanged digests today's code is a no-op, so the object would never be recorded.
**Options considered**: (1) turn the reconcile into a full apply, which rewrites every drifted object and so corrects drift the operator only reports (ADR-012, ADR-019); (2) treat the object as a restore does a missing one.
**Decision**: Option 2. For a ModuleInstance with unchanged digests the restore list is the restorable missing objects plus `TakenIn`. The restore applies exactly those and records the inventory of the rendered set, without expired Jobs and let-go objects. For a ModulePackage, which has no restore, the reconcile applies `Allowed`.
**Rationale**: It reuses the one path that already applies a part of the render with unchanged digests, and it keeps "an object that exists and is in the inventory is never rewritten on unchanged digests". The main requirement "A missing object is restored" says the step applies the missing objects "and only those"; the `drift-detection` delta modifies it to name the taken-in objects.

What `Drifted` says: a taken-in object is left out of the dry-run diff of the reconcile that takes it in. That reconcile applies it, so a difference between it and the render is about to be closed and is not drift; without this rule a restore would keep `Drifted=True` on an object the operator has just written, until the next render. From the next render on the object is in the inventory and is compared like any other.

A taken-in object is never deleted and created again, and two locks keep that. The second lock: the apply hands the taken-in objects to the resource manager's delete guard (`internal/apply/claims.go`), which refuses to delete one of them, as it refuses a claim, so an object that changes between the check below and the staged apply is not recreated. The check sends the request Flux sends and judges the answer with the predicate Flux uses (`ssaerrors.IsImmutableError`), which is true for every Invalid or Conflict answer: a render the API server rejects for another reason fails the reconcile here too, which is the safe side. The first lock: with `spec.rollout.forceConflicts`, Flux recreates an object whose update the API server refuses, and the adopt annotation exists so that a user need not delete an object to bring it under OPM. So before the first write of a forced apply the controller sends the dry-run for each taken-in object, as `checkClaims` does for claims (`internal/apply/claims.go:70-97`), and when the API server refuses the update as immutable the reconcile fails with nothing written: `Ready=False` `ApplyFailed`, on the backoff, with a message that names the object and the refused fields and says that OPM does not recreate an object it is taking in. The way out is the user's: change the object so the update is accepted, or delete it. Once the object is in the inventory, `forceConflicts` treats it like every other object of the instance.

#### Which identity the guard asks with

**Context**: The delete half's design recorded "the guard's `InstanceUUID` is the render's; the window of an identity change needs no second identity on the apply side". The first form of this proposal asked a second time with the earlier identity while a change is not settled, so that a kept claim or an object adopted under the earlier identity stayed the instance's own in the window. The build's first check showed that the earlier identity is not the instance's alone: an identity is the UUID of `registryPath:name:namespace` (core `module_instance.cue`), the kind of the record is no input, and a ModulePackage renders an authored instance whose path, name and namespace come from its artifact. So another record can render identity A while the instance is in its window, and the second question would allow a write over that record's object.
**Options considered**: (1) the render's identity alone, as the archived design recorded and as the cli does; (2) ask again with the earlier identity on an ownership refusal; (3) ask again only for an object the instance can prove was its own (no such proof is in status).
**Decision**: Option 1 (supervisor's gate, 2026-10-09, after the build's check). One identity, one question per object:

| State | The guard asks with |
| --- | --- |
| No identity change pending (also: nothing recorded yet, for a ModuleInstance and a ModulePackage alike) | the instance's identity |
| An identity change is not settled | the new identity, and only that one |

"The instance's identity" is `identityPlan.InstanceUUID`: the render's, or the recorded one when the render carries none. The guard is never handed `identityPlan.PreviousInstanceUUID` and never `identityPlan.Prune`: for a ModulePackage with nothing recorded that list ends with the empty identity (`internal/reconcile/identity.go:66-71`), a fallback that exists for the prune alone. The guard MUST never ask with an empty identity: the library answers an empty identity with "apply" for an inventoried object whose adopt annotation names another instance (`apply.go:139-141`), so the package would write over an object it must let go. `Guard` returns an error for an empty identity, and a reconcile that has an object to apply and no identity at all (nothing recorded and a render without the UUID label) fails as a failed apply with nothing written. An empty apply list is not judged and needs no identity.
**Rationale**: When the operator cannot prove an object is its own, it writes nothing to it. What is lost is small. An inventoried object is applied whatever its UUID label says (rows 3 and 5), so the instance's recorded objects pass an identity change untouched. Only an object outside the inventory that carries the earlier identity is affected (row 4: a kept claim the render names again, an object a failed apply created before the change): the reconcile is refused as `other-instance` until the object is annotated for the new identity or removed, and the refusal prints the annotation. And an object adopted under the earlier identity is let go at once (row 11).

Apply and prune do not ask alike, and need not. The prune asks with both identities (`internal/apply/prune.go:196-212`), because a delete that is skipped is the safe answer and a relabel that did not finish must not strand the instance's own objects. The apply asks with one, because a write that is refused is the safe answer. For an object adopted under the earlier identity the two agree: the guard lets it go (`adopted-elsewhere`), and the prune and a deletion cleanup leave it behind as `adopted-elsewhere` (`TestPruneAdoptAnnotationAndIdentities`: "No single identity lets this delete proceed"). It is never written and never deleted, in the window or after it.

#### The annotation that names the earlier identity

**Context**: The delete half left this open. An instance adopted an object under identity A, so the object carries `opmodel.dev/adopt=A`. The module path changes and the identity becomes B. The contract says the object "must be re-annotated with the new UUID" (0012:D8, alternatives; `ApplyInput.InInventory` doc, `apply.go:41-51`). On the delete side the object is left behind as `adopted-elsewhere` in the window and after it (`reports/T3.1.md`, pinned in `TestPruneAdoptAnnotationAndIdentities`).
**Options considered**:
1. Do nothing more than the contract: the instance lets the object go on its first render under B. This is what the cli does.
2. Accept the annotation through the earlier identity while the change is not settled, and let the object go once it is settled. No write to the annotation.
3. The operator rewrites the annotation from A to B in the apply that relabels the object. The object stays the instance's for good.
4. The operator removes the annotation once the object is in its inventory. The library's hand-over then breaks: the instance that let the object go no longer sees `adopted-elsewhere` and refuses its whole reconcile as `other-instance`.
5. The library learns an instance's earlier identities and judges the annotation against them, for apply and delete.
**Decision**: Option 1. The operator MUST NOT write the adopt annotation, and it asks with one identity ("Which identity the guard asks with").
**Rationale**: Options 3 and 4 make a frontend write the annotation, which 0012:D8:R6 forbids ("Neither frontend sets the adopt annotation on the user's behalf"), and the annotation is the user's record of consent on the object. Option 5 is the real fix and is not the operator's to make. Option 2 was the first form of this proposal; it needs the second question, which can reach another record's object, and on the healthy path it gave one reconcile more before the object was let go all the same. So on the first render under B the verdict says `adopted-elsewhere` for the inventoried object: it is left out of the apply, leaves the inventory, and is not deleted. The `AdoptedElsewhere` event prints the exact annotation to set (`... to take it back, annotate it opmodel.dev/adopt=B`), so the way back is one command; the next render takes the object in again. The delete half agrees: it never deletes this object, in the window or after it (`TestPruneAdoptAnnotationAndIdentities`). Ruled at the supervisor's gate on 2026-10-09: no revision of 0012:D8:R6 and no library change.

#### The class of a refusal

**Options considered**: (1) `Stalled`, rechecked on the long interval; (2) `Ready=False`, not `Stalled`, on the bounded backoff.
**Decision**: Option 2, reason `ApplyRefused`.
**Rationale**: The remedy is on another object (an annotation, a deletion that finishes, another instance that stops rendering the object), and no watch of the instance reports it. The backoff (capped at five minutes) finds it. `ClaimConflict` and `DependentsRemain` have this class for the same reason (`moduleinstance.go:655-659`, `:831-834`).

#### A failed read

**Context**: Today Flux tolerates a failed read before its apply, so a ServiceAccount that may patch a kind and not read it applies. Drift detection already fails on a Forbidden read (`drift.go:96-109`, #265).
**Decision**: A reconcile that would write MUST NOT write when the guard could not read an object for a reason other than "not found": it fails as a failed apply does today (`markApplyFailure`, `moduleinstance.go:820-827`): `Stalled` with `ImpersonationFailed` when the read is Forbidden under an effective ServiceAccount, `Ready=False` `ApplyFailed` with backoff otherwise. A reconcile with unchanged digests whose guard read fails restores nothing and takes nothing in; it reports the failed read as the drift check does today (`Drifted=Unknown` with `DriftCheckForbidden` on Forbidden, a counted drift failure otherwise) and lets nothing go. That is a ModuleInstance. A ModulePackage has no drift check: its reconcile fails, as stated under "Where the guard runs".
One read answer counts as "no live object": the API server does not serve the kind yet and a CustomResourceDefinition of the same apply list defines it (`pendingCRDKind`, `apply.go:167-187`). Such an object cannot exist. Every other "kind not served" answer fails the read.
**Rationale**: A verdict on a read that failed would be a guess. Keeping the outcome of a no-write reconcile as it is today keeps #265 unchanged.

#### The reading identity

**Options considered**: (1) the identity that applies; (2) the operator's own identity, which can read more.
**Decision**: Option 1, as the delete half decided for its reads and #265 for drift.
**Rationale**: A refusal message names the owner of an object. Read with the operator's rights, it would tell a tenant who owns an object the tenant's ServiceAccount may not read.

#### Cost

**Context**: On a reconcile that renders, a ModuleInstance today reads each object of the apply list twice and sends one dry-run patch for it: `refusedRead` (`drift.go:63`), then Flux's `Diff` (`drift.go:67`). A changed render adds Flux's own reads in the staged apply.
**Decision**: The guard's GET replaces `refusedRead`'s GET: `DetectDrift` takes the guard's result and makes no read of its own before `Diff`. A ModuleInstance therefore makes no extra request. A ModulePackage, which has no drift detection, makes one GET per rendered object on each reconcile that renders. A reconcile that skips its render because its inputs are unchanged (`moduleinstance.go:750-761`, `modulepackage.go:311`) runs no guard, as it runs no drift detection: a new adopt annotation is seen at the next render, at the latest after `--drift-render-interval`.
**Rationale**: The periodic reconcile of a healthy instance stays what it costs today. The guard's reads are live reads as the identity that applies: the impersonated client when a ServiceAccount is effective (it has no cache), and the manager's uncached reader (`APIReader`) otherwise, the reader the health judgement names for the same reason (`appliedReader`, `moduleinstance.go:708`). A cached read could hide a new annotation.

Not counted above: an instance that stays refused renders on every retry of the backoff (capped at five minutes), where a healthy one renders once per drift render interval. Many long-lived refusals after an upgrade cost that many renders; the render slot pool bounds them.

#### How a let-go object stays visible

**Options considered**: (1) one event when the object is first let go, and nothing after; (2) an event on every reconcile that renders it; (3) the count in the message of `Ready=True`, and an event when the count changes; (4) a condition of its own; (5) a status field that lists let-go objects (a CRD change).
**Decision**: Option 3 (supervisor's gate, 2026-10-09; the count is what the design review of the delete half asked for).
**Rationale**: An event is not durable: the API server drops it after its retention time, and the client folds repeats only when they are at most six minutes apart, so option 2 writes a new Event at every render and still leaves gaps (a long drift render interval, a suspended instance, a render that fails). The `Ready` message is in the object's status and is rewritten by every reconcile that renders, so `kubectl get` and `kubectl describe` show the state without an event. The state is not always one the user chose: a principal with patch rights on one object can set it (Security), which is why it must not depend on events. Option 4 adds a condition type and option 5 an API field for what one sentence carries. Limits, stated: the message holds the count, not the names; the names are in the event and in the log line of each reconcile. A reconcile that skips its render keeps the message of the last render.

### Where the operator differs from the cli

Mirrored from `guard-every-apply-by-ownership` in the cli: one pass before the first write; all-or-nothing refusal; `adopted-elsewhere` alone lets go; the stale set is computed from the full render; the library's message is never reworded; fail closed on a failed read; a closed list of write call sites.

Different, with the reason:

- **No-write reconciles.** The cli always applies. The operator also reconciles with nothing to write, so it needs the rule "would write, or exists outside the inventory" and the taking in through the restore.
- **One identity, as the cli.** The operator stores two identities until a change is settled, for the prune. The apply guard asks with the new one only.
- **Conditions and events, not exit codes.** `ApplyRefused` is exit 1; `AdoptedElsewhere` is the warning line.
- **Read as the tenant.** The cli reads as the user who runs it. The operator reads as the ServiceAccount it impersonates, so a refusal never reveals more than that account may read.
- **No dry run, no install, no `Admit`, no `--create-namespace`.** The operator has none of these paths. Its Namespace is part of the apply list and is judged like any object.
- **The cli's extra line for a first apply with no record** ("the objects may be the instance's own under an earlier identity") has no counterpart: the operator's record is the object's own status. The case "deleted without pruning, created again with another module path" is in the docs page instead.
- **The forced recreate.** The cli never deletes inside an apply. In the operator the guard has allowed every object before Flux can recreate one; the delete keeps its UID precondition and asks nothing more.

### Breaking changes and the way out

| What | Before | After | Way out the docs name |
| --- | --- | --- | --- |
| A render names an object that exists and OPM does not manage (created by hand, by Helm, by another controller), not in the inventory | applied over, labels set | `ApplyRefused`, `foreign-object` | Annotate it `opmodel.dev/adopt=<UUID>`; the message prints the line. Or remove the object, or the component that renders it. |
| Two instances render the same object (a shared Namespace, CRD or ConfigMap) and the second meets it for the first time after the upgrade | both apply, each relabels | the second is refused, `other-instance` | Render the object in one instance only, or hand it over with the annotation. Instances that already share an object in both inventories are not refused. |
| Leftovers of a deleted instance (kept claims, Namespaces, CRDs, everything when `spec.prune` is false or after an orphan exit) met by an instance with another name, namespace or module path | applied over | `ApplyRefused`, `other-instance` | Annotate each, or delete the leftovers. |
| An object of the apply list is being deleted and the reconcile would write it | the patch is accepted | `ApplyRefused`, `terminating`, until the object is gone | Release the object outside OPM; the retry applies. |
| A tenant ServiceAccount may patch a kind and not `get` it | applied | `Stalled`, `ImpersonationFailed` | Grant `get` on the kind. |
| A ModulePackage with matching digests whose ServiceAccount is missing or may not read a rendered kind | no-op, `Ready=True` | `Stalled`, `ImpersonationFailed`; nothing written | Restore the ServiceAccount or grant `get`. |
| A module renders an object Kubernetes creates by itself (the `default` ServiceAccount of a namespace), not in the inventory | applied over | `ApplyRefused`, `foreign-object` | Annotate it, or do not render it. |
| `forceConflicts` and an adopted object whose immutable fields differ from the render | not applicable | `ApplyFailed`, nothing written; the object is not recreated | Change the object, or delete it. |
| `forceConflicts` and an object of the instance's own that is outside the inventory (it carries the instance's UUID label, or OPM's managed-by label and no UUID label: what a first apply created before it failed, a kept claim), whose immutable fields differ from the render | deleted and created again | `ApplyFailed`, nothing written; the object is not recreated, because every allowed object outside the inventory is taken in, whatever allowed it | Delete the object yourself, or change it so the update is accepted. Once the object is in the inventory, `forceConflicts` recreates it as before. |
| An object's adopt annotation names another instance | applied over | let go, `AdoptedElsewhere`, out of the inventory, not deleted | Intended. To take it back, set the annotation to this instance's UUID. |
| An object adopted under an earlier identity, from the first render under the new identity | not applicable (no annotation was read) | let go, as the row above | Set the annotation to the new UUID. |
| A render that holds objects, none of which carries the UUID label, for an instance or package with no `status.instanceUUID` | applied | `ApplyFailed`, nothing written: the verdict is never asked without an identity | No render of the shipped catalog is known to reach this row: every transformer of `catalog_opm` stamps `#context.labels`, which hold the UUID, the TransformerRegistration transformer included (read in source on 2026-10-09, not rendered). A transformer of another catalog that sets no context labels can reach it; that catalog must stamp the label. A render that holds no object (every component switched off) is not failed: it has nothing to judge. |
| A rendered object outside the inventory that carries the instance's earlier identity, while the identity change is not settled (a kept claim the render names again, an object a failed apply created) | applied over, relabelled | `ApplyRefused`, `other-instance` | Annotate it `opmodel.dev/adopt=<new UUID>`, or remove it. |

The instance UUID is the one every refusal message prints: the identity of the render. It is `status.instanceUUID` of the ModuleInstance or ModulePackage, except while a first apply or a change of identity is refused: a refused reconcile stores no identity, so the status then holds none or the earlier one.

### Security

This is the threat statement the delete half's design review asked this change to carry.

- Assets: objects in the cluster that an instance did not create, and their data; the instance's own objects.
- Trust boundary: the cluster API. Live labels and annotations are input that any principal with patch rights on the object can set. `status.inventory` and the two identity fields are in the status subresource of the operator's own kinds.
- Threats and mitigations:
  - Taking over another owner's object on apply: the verdict on every object of the write list, before the first write, on a fresh read. Baseline: the contract of 0012:D8 and the library's verdict tests.
  - Deciding on a read that failed: fails closed.
  - Learning the owner of an object the tenant may not read: the read is made as the identity that applies.
  - A write that bypasses the guard: the closed list of write call sites.
  - Writing over an object that must be let go because the question was asked with no identity: the guard never asks with an empty identity.
  - Deleting an object the user handed over, through a forced recreate: a taken-in object is never recreated ("Taking an object in"). Two locks: the dry-run before the first write, and the resource manager's delete guard, which refuses the delete of an object the reconcile takes in, so an object that changes between the check and the staged apply is not deleted either.
  - Writing over another record's object because it carries an identity this instance once had: the guard asks with the instance's current identity only, never with the earlier one.
- Residual risks. Owner: the operator maintainers. Each was accepted at the supervisor's gate on 2026-10-09 (the window cases below: one in advance, one after the build's review) and has a revisit trigger.
  - A principal with patch rights on one object can set the adopt annotation to another UUID. The instance then stops applying that object while it stays `Ready=True`, and unlike removed labels the next apply does not undo it. Patch rights do not include the right to change the instance, so for some principals this is more than they could do before. Detection: the count in the `Ready` message and the `AdoptedElsewhere` event. Revisit when a tenant reports an object let go that nobody handed over, or when the platform gains admission control for the annotation.
  - Two records that render one identity (existing, not made by this change; accepted at the supervisor's gate on 2026-10-09). The instance identity is the UUID of the module's registry path, the instance name and the namespace. The kind of the record is no input, and a ModulePackage takes all three from the `instance.cue` of its artifact. So a ModulePackage, a ModuleInstance and a cli instance can render the same identity, and each then counts the other's objects as its own: outside the inventory the verdict allows an OPM object that carries the asking identity (row 2), on apply and on delete. What the operator refuses today is another thing: `DuplicateIdentities` (0015:D15, opm-operator#148; `internal/render/kernel_module_renderer.go`, main spec `modulepackage-kernel-rendering`) refuses a render in which two objects share one apply identity (kind, namespace and name). It compares objects inside one render and never the instance identity of two records. This change does not widen the case (the guard asks with the current identity only) and does not close it. Revisit when two records are found to render one identity in a cluster, or when the operator gains a check that refuses a record whose rendered identity another record of the cluster already holds (a follow-up change).
  - The same principal can set the annotation to this instance's UUID on a foreign object, and the instance then takes it over. That is the contract's adopt, and it needs patch rights on the object taken. Revisit with any revision of 0012:D8:R2.
  - A principal who may create an object under a name the instance renders, while that object is not in the inventory, turns a Ready instance to `Ready=False` `ApplyRefused`, also with unchanged digests, and the instance restores nothing while it is refused. Before this change the same act changed no condition and the next apply took the object over. A rendered Job with a TTL leaves the inventory when it expires, so its name is always open to this. The refusal stays: the operator does not write over an object it cannot prove is its own, and a refusal that names the object is the fail-safe answer. Way out: delete the object, or annotate it for the instance. Revisit when an instance is refused this way in a namespace its tenants share with others, or when a Job with a TTL is refused in practice.
  - A principal who may write `status` of the instance can add an entry to `status.inventory`; the guard then judges that object as inventoried and applies over it. Such a principal can already name any object for deletion (the delete half's residual risk). No new right is gained. Revisit if write access to the status subresource is ever granted to tenants.
  - The window between the guard's read and the patch is closed for a taken-in object only: the apply sends it with the UID that was read ("What the build decided", 1). It stays open in two cases. First (2026-10-09, accepted at the gate in advance): an object that did not exist at the read and that another writer creates under the name before the write is applied over, because server-side apply has no create-only form and Flux's staged apply offers none. Two records that render one new object and reconcile in the same moment can therefore both read it as absent, both apply it and both list it. Second (decided by the build and accepted at the supervisor's gate on 2026-10-09, after the review): an inventoried object that another writer deletes and creates again between the read and the write is applied over as well. It is not pinned, because a pinned UID makes a later forced recreate of the instance's own object fail, and the case adds little: an inventoried object is applied whatever its labels say (rows 5 and 8), so a replacement before the read is applied over too. The cli has the same window. Revisit when the library or Flux offers an apply with a precondition, or when an object is found written over in this window.
- Messages carry object names and instance UUIDs. Neither is a secret. No message carries an enhancement reference.

## Risks / Trade-offs

- [A shared Namespace or CRD] -> The most likely new refusal. The migration note and the docs page lead with it. Instances that already share the object in both inventories keep applying; only a new meeting is refused.
- [An inventoried object stuck terminating blocks a changed render] -> For example a claim held by pvc-protection while the instance's own pods mount it. The operator cannot change the workload that would release it. Remedy outside OPM. This is 0012:D8:R5 and the cli has it.
- [An adopted object is let go at the first render after `spec.module.path` changes] -> Documented, with the annotation to set in the event.
- [A kept claim or a leftover of a failed apply refuses the reconcile during an identity change] -> It is outside the inventory and carries the earlier identity, which the guard does not ask with. The refusal prints the annotation to set. Documented in the docs page.
- [An object let go and then adopted, whose annotation is later removed] -> It carries the adopter's identity outside this instance's inventory: `other-instance`, the reconcile is refused until the module stops rendering it or it is annotated back. Intended: the instance renders an object another instance holds.
- [A let-go entry stays in the inventory while the reconcile fails for another reason] -> The inventory is replaced only by a commit that records one. Until then the delete verdict skips the object on a deletion (`adopted-elsewhere`).
- [An instance with no UUID in its render] -> With a recorded identity the guard asks with that one. With none recorded, a reconcile that has an object to apply fails as a failed apply and writes nothing, whatever the digests say; an empty apply list asks nothing and passes ("What the build decided", 2).
- [The fake client and labels] -> The guard's tests of the refusal run against the library's verdict with real objects in envtest; unit tests cover the plan.
- [A module that renders an object Kubernetes creates by itself] -> For example the `default` ServiceAccount of its namespace. A new instance, or a first apply that failed after the Namespace stage, is refused as `foreign-object` until the object is annotated. It follows from 0012:D8:R1 and is in the docs page.
- [A ModulePackage that was a silent no-op now stalls] -> When its ServiceAccount is missing or may not read. Named in the migration note.

## What the build decided

Recorded on 2026-10-09 by the build. Each item is inside the accepted proposal; none changes a requirement.

1. **The second lock is the UID that was read, and the delete guard.** `Guard` returns a `Pin` (object and UID) for each taken-in object. `Apply` sets that UID as `metadata.uid` on a copy of the object it sends. The API server refuses a write that names another UID than the live object's (`metadata.uid: field is immutable`, shown in envtest) and a create that names one, so the apply reaches the object that was judged or fails. Under `forceConflicts` that refusal would start a recreate; the resource manager's delete guard refuses to delete an object the apply takes in, matched by group, kind, namespace and name, so an object that took the name is not deleted either. Four envtest cases pin it (`test/integration/reconcile/ownership_apply_test.go`, "is the object that was read, or the apply fails"). An object that was absent at the read has no such lock: see the residual risk under Security.
2. **No identity fails every reconcile that has an object to apply.** `Guard` returns `ErrNoIdentity`, and both reconcilers fail as a failed apply also when the digests are unchanged, as the `ssa-apply` requirement says. An empty apply list is the exception (ruled at the supervisor's gate on 2026-10-09): `Guard` returns an empty result before it looks at the identity, because with nothing to write there is no verdict to ask, and a render with every component switched off stays `Ready` as before this change. This gives one more breaking row (a render that holds objects, no UUID label and nothing recorded). The fixture of `internal/controller/transformerregistration_identity_test.go` renders a claim without the UUID label, which the catalog's transformer does not do; the fixture now renders one more object that carries the label.
3. **The dry-run of a taken-in object runs inside `apply.Apply`,** beside the claim check, so no caller can leave it out. It therefore runs after the reconcile stored a changed identity, as the claim check does. No object is written.
4. **`DetectDrift` keeps its signature.** It sends no read of its own; the reconciler hands it the objects the guard allowed, without the taken-in ones. A failed guard read is reported by the reconciler as the failed drift check.
5. **The `AdoptedElsewhere` event is emitted by a reconcile that ends `Ready=True`.** A refused or failed reconcile emits none, so a retry loop does not repeat it; the next success reports it once.
6. **A guard read error names the API reason** (`reading <object> before the apply (Forbidden): ...`): the text of a refused discovery request does not say it.
7. **A limit of the taken-in dry-run:** Flux removes the CA bundle of a CustomResourceDefinition before its own dry-run; the taken-in check does not. For a taken-in CRD with a CA bundle the API server rejects, the check fails the reconcile where the staged apply would have gone on. That is the safe side. Not tested.
8. **Fixtures of merged tests changed, no assertion weakened.** The render stubs of the controller suite and one unit test gained the UUID label every real render carries. `storeClaim` (shrink refusal specs) labels the stored claim as the provider's earlier apply did. "still reports drift on another resource while one is withheld" first applies the provider once, so the sidecar is in the inventory: without that record the refused reconcile takes the sidecar in, and a taken-in object is left out of that reconcile's drift comparison by the `drift-detection` requirement.

9. **Accepted as built at the supervisor's gate on 2026-10-09, after the review.** (a) Under `forceConflicts` no object outside the inventory is deleted and created again, the instance's own leftover of an unfinished apply included: such an object is not proven to be the instance's own while two records can render one identity, and a delete and create of it would be the worst outcome of that case. The way out is to delete the object by hand. (b) An inventoried object is not UID-pinned: the inventory names objects, not UIDs, an inventoried object is applied whatever its labels say, and a pin would make the forced recreate of the instance's own object fail. (c) A taken-in object is left out of the drift comparison of the reconcile that takes it in, on changed digests too: that reconcile applies it, so the difference is closed in the same pass. An instance with no recorded inventory therefore reports no drift on the objects it takes in.
10. **A ModulePackage that applies on matching digests is refused by an inventoried object that is being deleted** (ruled at the supervisor's gate on 2026-10-09: the `ssa-apply` sentence wins). Such a reconcile takes an object in or lets an inventoried one go, and a package has no restore step: it applies the rendered set, so it would write the terminating object. It reports `ApplyRefused` until the object is gone; the retry then applies the whole render and creates the object again. A package reconcile that stays a no-op, and a ModuleInstance restore, write nothing over an inventoried object and are not refused.

## Migration Plan

No stored data is migrated and no status field changes. Rollback is a revert: no stored state depends on the guard. An object that was let go is then applied again by the old code on its next changed render.

Migration note for the PR body and the release:

- The operator now checks who holds each object before it applies. A reconcile is refused, with nothing changed, when an object it renders exists and is not managed by OPM, belongs to another instance, or is being deleted. The object reports `Ready=False` with reason `ApplyRefused`.
- To let an instance take an existing object: `kubectl annotate <kind> <name> opmodel.dev/adopt=<instance UUID>`. The refusal prints the exact annotation. Use the UUID it prints: `status.instanceUUID` holds the same value once an apply has succeeded.
- An instance is refused when it meets an object another instance holds. Render a shared Namespace or CRD in one instance.
- An object whose `opmodel.dev/adopt` annotation names another instance is no longer applied and leaves the inventory. It is not deleted. The `Ready` message counts such objects and an `AdoptedElsewhere` event names them.
- With `spec.rollout.forceConflicts`, an object that exists and is not yet in the inventory is not deleted and created again: an object the instance adopts, and also its own leftover of an apply that did not finish. When its immutable fields differ from the render, the apply fails and names them; delete the object yourself or change it.
- After `spec.module.path` changes, set the adopt annotation of each object the instance once adopted to the new UUID: the instance lets such an object go at its first render under the new identity. An object outside the inventory that still carries the earlier identity (a kept claim the render names again) refuses the reconcile until it is annotated for the new UUID.
- A ServiceAccount the operator impersonates needs `get` on every kind it applies. A ModulePackage whose ServiceAccount is missing or lacks `get` now reports it also when nothing changed.
- A render that holds objects must carry the `module-instance.opmodel.dev/uuid` label on at least one of them. Without it, and with no `status.instanceUUID` recorded, the apply fails with `ApplyFailed`. Every transformer of the opm catalog sets the label; a render that holds no object is not affected.

## Open Questions

None is open. Ruled at the supervisor's gate on 2026-10-09:

1. **The adopt annotation that names an instance's earlier identity, and the second question.** The operator never writes the annotation, and the guard asks with the instance's current identity only. An object adopted under the earlier identity is let go at the first render under the new one, and the event prints the annotation to set. The first form of this proposal asked again with the earlier identity in the window; it was dropped after the build showed that another record can render that identity. No revision of 0012:D8:R6 and no library change.
2. **Visibility of let-go objects.** No CRD field. The count in the message of `Ready=True` and an `AdoptedElsewhere` event when the count changes.
3. **What the archived design recorded.** "One identity on the apply side" stands as recorded. One change to it stands: a refusal and a take-in also on unchanged digests.

The docs page: this change adds an "Ownership on apply" page under `docs/site/operating/` with the refusal table, the adopt annotation and the shared-object case, and links it from `deletion-and-pruning.md`.
