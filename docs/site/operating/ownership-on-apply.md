---
title: "Ownership on apply"
description: "What the operator checks before it applies an object, when it refuses, and how an instance takes over an object that exists."
type: explanation
weight: 32
---

Before the operator writes an object of a ModuleInstance or a ModulePackage, it reads the live object and asks one question: does this instance hold it? When the answer is no, the operator writes nothing in that reconcile. This page says what it checks, which applies it refuses, and the way out of each.

To see what the operator does on a delete, read [Deletion and pruning](/docs/operating/deletion-and-pruning/).

## What the operator checks

On every reconcile that renders, the operator reads each rendered object once, as the identity that applies (the ServiceAccount of the instance when one is set). It judges each object by four facts:

- whether the object exists;
- whether `status.inventory` of the instance lists it;
- its labels `app.kubernetes.io/managed-by` and `module-instance.opmodel.dev/uuid`, and its annotation `opmodel.dev/adopt`;
- whether it is being deleted.

The identity of the instance is `status.instanceUUID`. Every refusal prints it.

| The live object | The operator |
| --- | --- |
| Does not exist | Creates it. |
| Is in the inventory of the instance | Applies it, whatever its labels say. Labels that were removed or changed come back. |
| Is not in the inventory, is managed by OPM and carries this instance's UUID label, or no UUID label | Applies it and records it in the inventory. |
| Carries `opmodel.dev/adopt` with this instance's UUID | Applies it and records it in the inventory. This is the adopt. |
| Carries `opmodel.dev/adopt` with another instance's UUID | Lets it go: see "An object another instance adopted". |
| Is not in the inventory and OPM does not manage it | Refuses the reconcile. |
| Is not in the inventory and carries another instance's UUID label | Refuses the reconcile. |
| Is being deleted, and the reconcile would write it or the object is not in the inventory | Refuses the reconcile until the object is gone. |
| Cannot be read | Writes nothing and reports the failed read. |

## A refused reconcile

One refused object refuses the whole reconcile. Nothing is applied, nothing is pruned, and `status.inventory`, the applied digests and `status.instanceUUID` keep their values. The cluster never holds half of a render.

The object reports `Ready=False` with the reason `ApplyRefused`. It is not `Stalled`: the operator tries again on its backoff, 5 seconds doubling to 5 minutes, because the remedy is on another object and nothing tells the operator when it happened. One `Warning` event with the reason `ApplyRefused` carries the same text:

```text
Warning  ApplyRefused  Refused to apply over 1 object(s), nothing was applied: ConfigMap/media/settings exists and is not managed by OPM; to let this instance take it over, annotate it opmodel.dev/adopt=6f1c0a52-8f0e-5a0b-9d53-0c0a4f5f2b11.
```

The message names at most ten objects and then gives the number of the rest.

A reconcile that changes nothing (the render equals what was applied last) is refused only for an object that exists outside the inventory. An inventoried object that is being deleted does not refuse it: the operator writes nothing over that object.

## Take over an object that exists

Set the adopt annotation to the UUID of the instance. The refusal prints the exact annotation:

```sh
kubectl annotate configmap settings -n media opmodel.dev/adopt=6f1c0a52-8f0e-5a0b-9d53-0c0a4f5f2b11
```

The next attempt applies the object, sets the OPM labels and records it in the inventory. The operator never sets, changes or removes this annotation. It stays on the object as the record of the hand-over, so leave it there.

The operator does not delete and create again an object it takes over. With `spec.rollout.forceConflicts: true`, when the API server refuses the update of such an object (a changed immutable field, for example `spec.clusterIP` of a Service), the reconcile fails with the reason `ApplyFailed`, names the object and the refused fields, and writes nothing. Change the object so that the update is accepted, or delete it yourself. Once the object is in the inventory, `forceConflicts` treats it like every other object of the instance.

## The refusals and the way out

| What the instance renders | Reason in the message | Way out |
| --- | --- | --- |
| A shared Namespace, CustomResourceDefinition or ConfigMap that another instance already holds, met for the first time | `belongs to module instance <UUID>` | Render the object in one instance only. Or hand it over: annotate it for the instance that will hold it. Two instances that both listed the object in their inventory before the upgrade are not refused. |
| An object created by hand, by Helm or by another controller | `exists and is not managed by OPM` | Annotate it for the instance, or remove the object, or remove the component that renders it. |
| What a deleted instance left behind (a kept PersistentVolumeClaim, a Namespace, a CustomResourceDefinition, every object when `spec.prune` was false), met by an instance with another name, namespace or module path | `belongs to module instance <UUID>` | Annotate each object for the new instance, or delete the leftovers. |
| An object Kubernetes creates by itself, such as the `default` ServiceAccount of a namespace | `exists and is not managed by OPM` | Annotate it, or do not render it. |
| An object someone else creates under a name the instance renders, also a Job with `ttlSecondsAfterFinished` that left the inventory when it expired and was created again by another hand | `exists and is not managed by OPM`, or `belongs to module instance <UUID>` | Delete that object, or annotate it for the instance. |
| An object that is being deleted | `is being deleted` | Wait, or remove what holds the deletion (a finalizer, a workload that still mounts a claim). The retry applies. |

## An object another instance adopted

When the adopt annotation of a rendered object names another instance, this instance lets the object go. It does not apply it, does not compare it for drift, does not restore it and does not delete it. The object leaves `status.inventory`, the other objects are applied, and the instance stays `Ready=True`.

The message of `Ready` states how many rendered objects are in this state, for as long as the module renders them:

```text
Reconciliation succeeded. 1 rendered object(s) are adopted by another instance and are not applied.
```

One `Warning` event with the reason `AdoptedElsewhere` names the objects when that number changes. To take an object back, set its annotation to this instance's UUID; the event prints the annotation.

Anyone who may patch the object can set the annotation. So when the `Ready` message counts an object nobody handed over, look at who changed it.

### Hand an object from one instance to another

1. Annotate the object with the UUID of the instance that takes it: `opmodel.dev/adopt=<UUID of the new holder>`.
2. The instance that held it lets it go at its next render. The new holder applies it and records it at its next render.
3. Remove the object from the module of the first instance when you no longer want it counted there.

Do not remove the annotation while the first instance still renders the object: the object then carries the new holder's UUID label, and the first instance is refused with `belongs to module instance`.

## When the module path of an instance changes

A change of `spec.module.path` changes the identity of the instance. The objects in its inventory pass the change untouched: the operator applies them and sets the new UUID label. Two kinds of object need a hand:

- An object the instance adopted earlier carries the annotation with the earlier UUID. The instance lets it go at its first render under the new identity. Set the annotation to the new `status.instanceUUID` to take it back.
- An object outside the inventory that still carries the earlier UUID label, such as a kept PersistentVolumeClaim the render names again, refuses the reconcile with `belongs to module instance <earlier UUID>`. Annotate it with the new UUID, or remove it. The operator does not accept the earlier identity here, because another instance can render it.

A ModuleInstance that was deleted without pruning and created again with another module path meets its old objects in the same way: annotate them for the new instance.

## What a ServiceAccount needs

The read is made as the identity that applies. A ServiceAccount the operator impersonates therefore needs `get` on every kind it applies, beside `patch` and `create`. Without it the object reports `Stalled=True` with the reason `ImpersonationFailed` and nothing is written. A ModulePackage reports a missing ServiceAccount, or a kind it may not read, also when nothing changed in its source.

## A render without an instance identity

Every object a module renders carries the `module-instance.opmodel.dev/uuid` label. When no rendered object carries it and the object has no `status.instanceUUID` yet, the operator cannot ask who holds an object. The reconcile fails with the reason `ApplyFailed` and writes nothing.

## When the check runs

The check runs on every reconcile that renders. A reconcile that skips its render because nothing changed reads no object, so a new adopt annotation is seen at the next render: at the latest after `--drift-render-interval`.

<!-- Check against: opm-operator/internal/apply/guard.go, opm-operator/internal/apply/takein.go, opm-operator/internal/reconcile/ownership.go, opm-operator/internal/reconcile/moduleinstance.go, opm-operator/internal/reconcile/modulepackage.go, opm-operator/internal/status/ownership.go, library/opm/k8s/ownership/apply.go -->
