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

"Inventory" is `status.inventory` as read at the start of the reconcile, matched by group, kind, namespace and name. "Ours" is the render's identity. A is the earlier identity while a change is not settled (`status.previousInstanceUUID`, or the recorded identity when this render starts the change). Evidence for each verdict is the named test in `opm/k8s/ownership/apply_test.go` at v1.0.0-beta.7 and the branch in `apply.go`.

| # | Live object | In inventory | Library verdict | Operator action |
| --- | --- | --- | --- | --- |
| 1 | None | either | allowed (`apply.go:95-97`; "a new object is applied") | Apply (create). |
| 2 | OPM-managed, UUID label ours | either | allowed ("an inventoried object of this instance is applied", "an OPM object of this instance outside the inventory is applied") | Apply. Outside the inventory it is taken in: see "Taking an object in". |
| 3 | OPM-managed, UUID label A, change not settled | yes | allowed with ours (a UUID label alone never refuses in the inventory, `apply.go:139-146`) | Apply; the apply relabels it. |
| 4 | OPM-managed, UUID label A, change not settled | no | `other-instance` with ours; allowed with A | Apply and take in. The second question is the operator's, as in the prune. |
| 5 | OPM-managed, UUID label of another instance | yes | allowed (as row 3; the object was shared before the upgrade, or is ours under an identity that is no longer recorded) | Apply. The apply relabels it. Accepted: the contract judges an inventoried object by its annotation only. |
| 6 | OPM-managed, UUID label of another instance | no | `other-instance` (`apply.go:118-121`; "another instance's object outside the inventory is refused") | Refuse the reconcile. |
| 7 | Not OPM-managed (no managed-by label, or another manager's value) | no | `foreign-object` (`apply.go:109-112`; "a foreign object outside the inventory is refused", "an object with no managed-by label is foreign") | Refuse the reconcile. |
| 8 | Not OPM-managed | yes | allowed ("an inventoried foreign object is applied") | Apply. The apply sets the labels again. |
| 9 | OPM-managed, no UUID label | either | allowed ("an OPM object without a UUID label is applied") | Apply. |
| 10 | Annotated `opmodel.dev/adopt` = ours | either | allowed (`apply.go:103-105`; "adoption lifts a foreign refusal", "adoption lifts an other-instance refusal", "an annotation naming this instance takes an inventoried object back") | Apply and record. This is the adopt. |
| 11 | Annotated = A, change not settled | either | an ownership refusal with ours (`adopted-elsewhere`, or rows 6 and 7 outside the inventory); allowed with A | Apply. After the change is settled it is row 12; see "The annotation that names the earlier identity". |
| 12 | Annotated for another instance | yes | `adopted-elsewhere` (`apply.go:139-146`; "an inventoried object annotated for another instance is refused") | Let go. |
| 13 | Annotated for another instance, OPM-managed, UUID label ours, the annotated one or none | no | `adopted-elsewhere` (`apply.go:114-117`, `:122-125`; "a dropped object is not taken back on the next apply", "a handed-over object stays adopted-elsewhere after the adopter applies") | Let go (it is already out of the inventory). |
| 14 | Annotated for another instance, not OPM-managed, or UUID label of a third instance | no | `foreign-object` or `other-instance` ("an annotation naming another instance lifts nothing", "... does not lift other-instance") | Refuse the reconcile. |
| 15 | Being deleted | either, any label or annotation | `terminating` (`apply.go:99-101`; five tests) | Refuse a reconcile that would write the object. |
| 16 | Read fails, not "not found" | | no verdict | Fail safe: see "A failed read". |

What the two outcomes mean:

**Refuse the reconcile.** Nothing is written: no identity is stored, no object is applied, nothing is pruned. `Ready=False` with reason `ApplyRefused`; `Stalled` is not set; one `Warning` event `ApplyRefused` with action `Apply`. The condition message and the event carry the count and the library's message for each refused object, unchanged (at most ten, fewer past 1024 characters, then the number of the rest: the shape of `LeftBehind`). The attempt counts as a failed apply and is retried on the bounded backoff. `status.inventory`, the applied digests and both identity fields keep their values, so the next reconcile judges again. The refused object is untouched.

**Let go.** The object is left out of the apply list, of drift detection and of the restore. Its entry is left out of the inventory this reconcile records. The stale set is computed from the full render, so the prune never sees it; if it did, the delete verdict would skip it (`adopted-elsewhere`), which is the second lock. The other objects are applied and the reconcile can end `Ready=True`. One `Warning` event `AdoptedElsewhere`, action `Apply`, with the count and the library's messages. It is emitted by every reconcile that renders and finds such an object, so a let-go object stays visible for as long as the module renders it.

### Where the guard runs

```go
// internal/apply

// GuardInput is what one reconcile hands to the apply guard.
type GuardInput struct {
    // Resources is the apply list: the rendered set without withheld objects.
    Resources []*unstructured.Unstructured
    // Inventory is status.inventory as read at the start of the reconcile.
    Inventory []releasesv1alpha1.InventoryEntry
    // Identities are the render's identity, then the earlier one while an
    // identity change is not settled.
    Identities []string
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
  -> build the client that applies
  -> Guard(apply list, inventory, identities)          one GET per object, as the identity that applies
  -> drift detection over Allowed                      reuses the guard's read result; no GET of its own
  -> plan: no-op, restore (missing + TakenIn), or apply (changed digests, or an unsettled identity change)
  -> refuse when Refused holds an object of the write list, or one that exists outside the inventory
  -> store a changed identity -> Apply(write list) -> prune(stale set of the full render)
  -> commit: inventory = rendered - expired Jobs - LetGo
```

ModulePackage flow: the same guard, before the no-op decision of `modulepackage.go:364`, which means the client that applies is built before that decision and no longer inside `applyAndPruneModulePackage` (`modulepackage.go:747`). A ModulePackage has no drift detection and no restore, so `TakenIn` and a let-go object not yet out of the inventory make the reconcile an apply of `Allowed`.

Reconcile phase impact: Source and Render: none. Apply: the guard before the first write; the write list excludes let-go objects. Prune: none (the stale set and the delete verdict are the delete half's). Status: the reason `ApplyRefused`; an inventory without let-go objects, also on a no-op. Events: two reasons.

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

**Context**: An allowed object that exists and is not in the inventory (rows 2, 4, 9, 10 outside the inventory) must be applied and recorded: an adopted object, an object taken back after a hand-over, a kept claim the render names again. With unchanged digests today's code is a no-op, so the object would never be recorded.
**Options considered**: (1) turn the reconcile into a full apply, which rewrites every drifted object and so corrects drift the operator only reports (ADR-012, ADR-019); (2) treat the object as a restore does a missing one.
**Decision**: Option 2. For a ModuleInstance with unchanged digests the restore list is the restorable missing objects plus `TakenIn`. The restore applies exactly those and records the inventory of the rendered set, without expired Jobs and let-go objects. For a ModulePackage, which has no restore, the reconcile applies `Allowed`.
**Rationale**: It reuses the one path that already applies a part of the render with unchanged digests, and it keeps "an object that exists and is in the inventory is never rewritten on unchanged digests".

#### Which identities the guard asks with

**Context**: The delete half's design recorded "the guard's `InstanceUUID` is the render's; the window of an identity change needs no second identity on the apply side". That holds inside the inventory. It does not hold for row 4 (an object outside the inventory that carries the earlier identity: a kept claim, an object a failed apply created) and row 11 (an object adopted under the earlier identity). The status field's own text says: "While it is set, an object that carries either identity counts as the instance's own."
**Options considered**: (1) the render's identity alone, as recorded; (2) ask again with the earlier identity while a change is not settled, when the first answer is an ownership refusal (`foreign-object`, `other-instance` or `adopted-elsewhere`). `terminating` is never asked again.
**Decision**: Option 2. The identities are the render's, then `status.previousInstanceUUID` when set, or the recorded `status.instanceUUID` when this render starts the change. An object is allowed when either answer allows it; otherwise the first answer's reason and message are reported. This replaces the note in the archived design.
**Rationale**: It is the rule `judgeDelete` has (`internal/apply/prune.go:196-212`), so apply and prune agree on what is the instance's own during the window. Every answer is still the library's. With no change pending there is one identity and one question.

#### The annotation that names the earlier identity

**Context**: The delete half left this open. An instance adopted an object under identity A, so the object carries `opmodel.dev/adopt=A`. The module path changes and the identity becomes B. The contract says the object "must be re-annotated with the new UUID" (0012:D8, alternatives; `ApplyInput.InInventory` doc, `apply.go:41-51`). On the delete side the object is left behind as `adopted-elsewhere` in the window and after it (`reports/T3.1.md`, pinned in `TestPruneAdoptAnnotationAndIdentities`).
**Options considered**:
1. Do nothing more than the contract: the instance lets the object go on its first render under B. This is what the cli does.
2. Accept the annotation through the earlier identity while the change is not settled (the decision above), and let the object go once it is settled. No write to the annotation.
3. The operator rewrites the annotation from A to B in the apply that relabels the object. The object stays the instance's for good.
4. The operator removes the annotation once the object is in its inventory. The library's hand-over then breaks: the instance that let the object go no longer sees `adopted-elsewhere` and refuses its whole reconcile as `other-instance`.
5. The library learns an instance's earlier identities and judges the annotation against them, for apply and delete.
**Decision**: Option 2. The operator MUST NOT write the adopt annotation.
**Rationale**: Options 3 and 4 make a frontend write the annotation, which 0012:D8:R6 forbids ("Neither frontend sets the adopt annotation on the user's behalf"), and the annotation is the user's record of consent on the object; a rewrite would also make the operator a field manager of it. Option 5 is the real fix and is not the operator's to make: it changes the verdict's input and the contract's text, for both frontends. Option 2 costs nothing and gives the user the length of the window, and after it the `AdoptedElsewhere` event prints the exact annotation to set (`... to take it back, annotate it opmodel.dev/adopt=B`), so the way back is one command and nothing is deleted. What stays open: after the change is settled the instance stops managing an object it adopted, with a warning and `Ready=True`. That is put to the owner below.

#### The class of a refusal

**Options considered**: (1) `Stalled`, rechecked on the long interval; (2) `Ready=False`, not `Stalled`, on the bounded backoff.
**Decision**: Option 2, reason `ApplyRefused`.
**Rationale**: The remedy is on another object (an annotation, a deletion that finishes, another instance that stops rendering the object), and no watch of the instance reports it. The backoff (capped at five minutes) finds it. `ClaimConflict` and `DependentsRemain` have this class for the same reason (`moduleinstance.go:655-659`, `:831-834`).

#### A failed read

**Context**: Today Flux tolerates a failed read before its apply, so a ServiceAccount that may patch a kind and not read it applies. Drift detection already fails on a Forbidden read (`drift.go:96-109`, #265).
**Decision**: A reconcile that would write MUST NOT write when the guard could not read an object for a reason other than "not found": it fails as a failed apply does today (`markApplyFailure`, `moduleinstance.go:820-827`): `Stalled` with `ImpersonationFailed` when the read is Forbidden under an effective ServiceAccount, `Ready=False` `ApplyFailed` with backoff otherwise. A reconcile with unchanged digests whose guard read fails restores nothing and takes nothing in; it reports the failed read as the drift check does today (`Drifted=Unknown` with `DriftCheckForbidden` on Forbidden, a counted drift failure otherwise) and lets nothing go.
One read answer counts as "no live object": the API server does not serve the kind yet and a CustomResourceDefinition of the same apply list defines it (`pendingCRDKind`, `apply.go:167-187`). Such an object cannot exist. Every other "kind not served" answer fails the read.
**Rationale**: A verdict on a read that failed would be a guess. Keeping the outcome of a no-write reconcile as it is today keeps #265 unchanged.

#### The reading identity

**Options considered**: (1) the identity that applies; (2) the operator's own identity, which can read more.
**Decision**: Option 1, as the delete half decided for its reads and #265 for drift.
**Rationale**: A refusal message names the owner of an object. Read with the operator's rights, it would tell a tenant who owns an object the tenant's ServiceAccount may not read.

#### Cost

**Context**: On a reconcile that renders, a ModuleInstance today reads each object of the apply list twice and sends one dry-run patch for it: `refusedRead` (`drift.go:63`), then Flux's `Diff` (`drift.go:67`). A changed render adds Flux's own reads in the staged apply.
**Decision**: The guard's GET replaces `refusedRead`'s GET: `DetectDrift` takes the guard's result and makes no read of its own before `Diff`. A ModuleInstance therefore makes no extra request. A ModulePackage, which has no drift detection, makes one GET per rendered object on each reconcile that renders. A reconcile that skips its render because its inputs are unchanged (`moduleinstance.go:750-761`, `modulepackage.go:311`) runs no guard, as it runs no drift detection: a new adopt annotation is seen at the next render, at the latest after `--drift-render-interval`.
**Rationale**: The periodic reconcile of a healthy instance stays what it costs today. The guard's reads go through the client that applies, uncached, because a cached read could hide a new annotation and the impersonated client has no cache.

#### How a let-go object stays visible

**Options considered**: (1) one event when the object is first let go; (2) an event on every reconcile that renders it; (3) a condition; (4) a status field that lists let-go objects (a CRD change).
**Decision**: Option 2. The API server folds repeated events into one series.
**Rationale**: After the first let-go the object is in no status field, so a single event that expires would leave no trace. The cli warns on every apply. Options 3 and 4 add vocabulary or API for a state the user chose; if the owner wants a count in status, that is a later, additive change.

### Where the operator differs from the cli

Mirrored from `guard-every-apply-by-ownership` in the cli: one pass before the first write; all-or-nothing refusal; `adopted-elsewhere` alone lets go; the stale set is computed from the full render; the library's message is never reworded; fail closed on a failed read; a closed list of write call sites.

Different, with the reason:

- **No-write reconciles.** The cli always applies. The operator also reconciles with nothing to write, so it needs the rule "would write, or exists outside the inventory" and the taking in through the restore.
- **Two identities in the window.** The cli has one recorded identity. The operator stores both until a change is settled and asks with both.
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
| An object's adopt annotation names another instance | applied over | let go, `AdoptedElsewhere`, out of the inventory, not deleted | Intended. To take it back, set the annotation to this instance's UUID. |
| An object adopted under an earlier identity, after the identity change is settled | not applicable (no annotation was read) | let go, as the row above | Set the annotation to the new UUID. |

The instance UUID is `status.instanceUUID` of the ModuleInstance or ModulePackage, and every refusal message prints it.

### Security

This is the threat statement the delete half's design review asked this change to carry.

- Assets: objects in the cluster that an instance did not create, and their data; the instance's own objects.
- Trust boundary: the cluster API. Live labels and annotations are input that any principal with patch rights on the object can set. `status.inventory` and the two identity fields are in the status subresource of the operator's own kinds.
- Threats and mitigations:
  - Taking over another owner's object on apply: the verdict on every object of the write list, before the first write, on a fresh read. Baseline: the contract of 0012:D8 and the library's verdict tests.
  - Deciding on a read that failed: fails closed.
  - Learning the owner of an object the tenant may not read: the read is made as the identity that applies.
  - A write that bypasses the guard: the closed list of write call sites.
- Residual risk, owner: the operator maintainers.
  - A principal with patch rights on one object can set the adopt annotation to another UUID. The instance then stops applying that object while it stays `Ready=True`, and unlike removed labels the next apply does not undo it. Patch rights do not include the right to change the instance, so for some principals this is more than they could do before. Detection: the `AdoptedElsewhere` warning on every reconcile that renders.
  - The same principal can set the annotation to this instance's UUID on a foreign object, and the instance then takes it over. That is the contract's adopt, and it needs patch rights on the object taken.
  - A principal who may write `status` of the instance can add an entry to `status.inventory`; the guard then judges that object as inventoried and applies over it. Such a principal can already name any object for deletion (the delete half's residual risk). No new right is gained.
  - The window between the guard's read and the patch is not closed: server-side apply has no precondition on labels. The cli has the same window. Accepted.
- Messages carry object names and instance UUIDs. Neither is a secret. No message carries an enhancement reference.

## Risks / Trade-offs

- [A shared Namespace or CRD] -> The most likely new refusal. The migration note and the docs page lead with it. Instances that already share the object in both inventories keep applying; only a new meeting is refused.
- [An inventoried object stuck terminating blocks a changed render] -> For example a claim held by pvc-protection while the instance's own pods mount it. The operator cannot change the workload that would release it. Remedy outside OPM. This is 0012:D8:R5 and the cli has it.
- [An adopted object is let go after an identity change is settled] -> Documented, with the annotation to set in the event. See the question for the owner.
- [An object let go and then adopted, whose annotation is later removed] -> It carries the adopter's identity outside this instance's inventory: `other-instance`, the reconcile is refused until the module stops rendering it or it is annotated back. Intended: the instance renders an object another instance holds.
- [A let-go entry stays in the inventory while the reconcile fails for another reason] -> The inventory is replaced only by a commit that records one. Until then the delete verdict skips the object on a deletion (`adopted-elsewhere`).
- [An instance with no UUID in its render] -> The library compares no identity inside the inventory and refuses every labelled object outside it. The operator passes what it has and adds no rule.
- [The fake client and labels] -> The guard's tests of the refusal and of the two identities run against the library's verdict with real objects in envtest; unit tests cover the plan.
- [More event volume] -> One `AdoptedElsewhere` series per instance with let-go objects. Bounded by the number of instances.

## Migration Plan

No stored data is migrated and no status field changes. Rollback is a revert: no stored state depends on the guard. An object that was let go is then applied again by the old code on its next changed render.

Migration note for the PR body and the release:

- The operator now checks who holds each object before it applies. A reconcile is refused, with nothing changed, when an object it renders exists and is not managed by OPM, belongs to another instance, or is being deleted. The object reports `Ready=False` with reason `ApplyRefused`.
- To let an instance take an existing object: `kubectl annotate <kind> <name> opmodel.dev/adopt=<instance UUID>`. The refusal prints the exact annotation; the UUID is `status.instanceUUID`.
- Two instances can no longer start to share an object. Render a shared Namespace or CRD in one instance.
- An object whose `opmodel.dev/adopt` annotation names another instance is no longer applied and leaves the inventory. It is not deleted. The operator reports it with an `AdoptedElsewhere` event.
- After `spec.module.path` changes, set the adopt annotation of each object the instance once adopted to the new UUID, or the instance lets it go once the change is settled.
- A ServiceAccount the operator impersonates needs `get` on every kind it applies.

## Open Questions

For the owner, with a recommendation; the change is built for the recommended option in each.

1. **The adopt annotation that names an instance's earlier identity.** Options: (a) the operator never writes the annotation; it accepts the earlier identity until the change is settled and then lets the object go with a warning that prints the annotation to set (this proposal); (b) the operator rewrites the annotation to the new identity, which needs a dated revision of 0012:D8:R6 and binds the cli too; (c) a library change: the verdicts take an instance's earlier identities, for apply and delete, with a revision of 0012:D8:R8. Recommendation: (a) now, and (c) as a follow-up in the library if the leftover is met in practice. (b) is not recommended: the annotation is the user's record.
2. **A count or list of let-go objects in status.** Options: (a) none, events only (this proposal); (b) an additive status field, which is a CRD change and releases the operator module. Recommendation: (a).

For the supervisor's gate:

- The archived design recorded "the guard's `InstanceUUID` is the render's" for the apply side. This proposal asks with both identities during the window ("Which identities the guard asks with"). It widens what is allowed in the window and narrows nothing.
- The docs page: this change adds an "Ownership on apply" page under `docs/site/operating/` with the refusal table, the adopt annotation and the shared-object case, and links it from `deletion-and-pruning.md`.
