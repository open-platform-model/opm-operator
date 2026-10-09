# ssa-apply Specification

## Purpose

The `internal/apply` package applies rendered resources to the cluster with Server-Side Apply as the `opm-controller` field manager, in Flux's `ApplyAllStaged` stages, and reports how many resources it created, updated and left unchanged.

## Requirements

### Requirement: SSA apply with opm-controller field manager
The `internal/apply` package MUST apply resources using Server-Side Apply with field manager name `opm-controller`.

#### Scenario: Successful apply
- **WHEN** a set of valid Kubernetes resources is applied
- **THEN** the resources exist in the cluster with `opm-controller` as the field manager

#### Scenario: Force enables immutable field recreation
- **WHEN** `force` is true and an object has an immutable field change
- **THEN** the apply succeeds by deleting and recreating the object

#### Scenario: Different field manager can overwrite fields
- **WHEN** another field manager owns a field and a second manager applies a change
- **THEN** the apply succeeds (Flux always applies with ForceOwnership, so SSA ownership conflicts do not surface through this layer)

### Requirement: Staged apply ordering
Resources MUST be applied using Flux's `ApplyAllStaged`. It applies cluster definitions (CRDs, Namespaces, ClusterRoles) first and waits for them to become ready. It then applies class definitions and waits for them, then any custom-stage kinds, then everything else. A CRD is ready once its `Established` condition is True. Discovery of its kind can lag that condition; the requirement "Custom resources wait for discovery of a CRD in the same set" covers that lag.

#### Scenario: CRD applied before custom resource
- **WHEN** the resource set contains both a CRD and an instance of that CRD
- **THEN** the CRD is applied in the cluster definitions stage before the instance in the default stage

#### Scenario: Namespace applied before namespaced resource
- **WHEN** the resource set contains a Namespace and resources in that namespace
- **THEN** the Namespace is applied in the cluster definitions stage before the namespaced resources

### Requirement: Apply result
The `Apply` function MUST return an `ApplyResult` with counts of created, updated, and unchanged resources.

#### Scenario: Mixed result
- **WHEN** applying a set where some resources are new and some already exist unchanged
- **THEN** the `ApplyResult` reflects the correct counts for each category

### Requirement: Custom resources wait for discovery of a CRD in the same set
When a staged apply fails only because the API server does not serve a kind yet, and a `CustomResourceDefinition` in the same resource set defines that kind (or, when discovery reports only the group, a group that a CRD in the set defines), the `internal/apply` package MUST retry the staged apply. A CRD can report `Established` before API discovery serves its kind. It MUST retry at a fixed interval, MUST NOT start a new attempt once a bounded time (10 seconds) has passed since the first retryable failure, and MUST give every attempt the caller's context unchanged. It MUST NOT retry any other error. When the bound or the caller's context ends while the error is still a no-match, `Apply` MUST return that no-match error, wrapped as any other apply failure is.

#### Scenario: Discovery serves the new kind late
- **WHEN** the resource set contains a CRD and an instance of it, and API discovery does not serve the instance's kind until some time after the CRD is `Established`, but within the bound
- **THEN** `Apply` succeeds without returning an error
- **AND** the instance exists in the cluster

#### Scenario: Custom resource without its CRD in the set fails at once
- **WHEN** the resource set contains a custom resource whose kind the API server does not serve, and no CRD in the set defines that kind or its group
- **THEN** `Apply` returns the no-match error without waiting for the retry bound

#### Scenario: Discovery never serves the kind
- **WHEN** the resource set contains a CRD and an instance of it, and API discovery does not serve the instance's kind before the bound or the caller's context ends
- **THEN** `Apply` returns an error that wraps the no-match error for that kind
- **AND** the reconcile reports it as `ApplyFailed`, as for any other apply failure

#### Scenario: Other apply errors are not retried
- **WHEN** a staged apply fails with an error that is not a no-match error, even though the set contains a CRD
- **THEN** `Apply` returns that error after one attempt

#### Scenario: An attempt is not bounded by the retry window
- **WHEN** a staged apply attempt, first or retried, runs while the caller's context has no deadline
- **THEN** the context the attempt sees has no deadline either

### Requirement: Apply result counts across a discovery retry
When `Apply` retries a staged apply, the `ApplyResult` MUST count each object by the first attempt that created or configured it, so a retry never reports an object that this call created or configured as unchanged.

#### Scenario: CRD created by the first attempt
- **WHEN** the first attempt creates a CRD and fails on its instance, and the retry sees the CRD unchanged and creates the instance
- **THEN** the `ApplyResult` counts both the CRD and the instance as created

### Requirement: A forced recreate keeps PersistentVolumeClaims unless data deletion is allowed
The apply MUST take, beside `force`, an option that allows the deletion of data, and the option MUST be off at its zero value. When `force` is true and the option is off, the apply MUST NOT delete a PersistentVolumeClaim of the core API group. Before it applies or deletes any object, it MUST check every such claim of the resource set that exists in the cluster. When the API server refuses the update of one (the error the forced recreate would answer with a delete), the apply MUST return an error that names the claim's namespace and name, the refused field or fields as the API server reports them, and the API server's message. In that case no object of the resource set is applied, created or deleted.

When the option is on, a forced recreate MUST delete and recreate a claim as it does any other object. When `force` is false, the option has no effect. The forced recreate of every other kind MUST delete and recreate as before, with the precondition of the requirement "A forced recreate deletes exactly the object that was read".

The resource manager MUST refuse on its own to delete a core PersistentVolumeClaim during an apply that does not allow the deletion of data, so that a claim that changes between the check and the apply is still kept.

#### Scenario: A claim with a changed immutable field is kept
- **GIVEN** a live PersistentVolumeClaim and a resource set that holds the claim with another `storageClassName` and a ConfigMap with new data
- **WHEN** the set is applied with `force` true and the data option off
- **THEN** the apply returns an error that names the claim and the field `spec`
- **AND** the claim has the UID it had, and no deletion timestamp
- **AND** the ConfigMap is unchanged

#### Scenario: The claim is recreated when data deletion is allowed
- **GIVEN** the same claim and resource set
- **WHEN** the set is applied with `force` true and the data option on
- **THEN** the apply succeeds and the claim has a new UID and the new `storageClassName`

#### Scenario: Another kind is recreated as before
- **GIVEN** a live immutable ConfigMap and a resource set that changes its data
- **WHEN** the set is applied with `force` true and the data option off
- **THEN** the apply succeeds and the ConfigMap has a new UID and the new data

#### Scenario: A claim that needs no recreate is applied
- **GIVEN** a live claim and a resource set that changes only a label of the claim
- **WHEN** the set is applied with `force` true and the data option off
- **THEN** the apply succeeds and the claim keeps its UID

#### Scenario: The resource manager refuses a claim delete
- **GIVEN** a resource manager built by the apply package
- **WHEN** its client is asked to delete a core PersistentVolumeClaim outside an apply that allows the deletion of data
- **THEN** the delete is refused and the claim is untouched

### Requirement: The staged apply order never contradicts the library's kind weights
The order in which the staged apply writes two objects of different kinds MUST NOT be the opposite of the order the library's kind weights give them. When Flux's staged apply, at the version the operator pins, applies an object of kind A before an object of kind B, the library's weight of A, at the version the operator pins, MUST NOT be higher than its weight of B. Flux's order MAY refine the library's order: it MAY put two kinds that the library weighs equally in an order of its own. Source: 0012:D5:R1.

The operator's test suite MUST fail when the two pinned versions break this rule. The check MUST read Flux's order from the pinned Flux module itself (its stage rules and its sort), not from a copy of Flux's kind list, so that a bump of the Flux pin that moves a kind fails the suite without an edit to the check. The check MUST cover every kind Flux's order names, every kind the library's weight table names, the same kind names in an API group of another owner, a custom kind and a custom kind whose name ends in `Class`.

These cases are not a failure by themselves, and the check MUST report them instead of hiding them: two kinds of equal library weight that Flux orders; a kind that only one of the two orders names, which is compared by the weight the library's fallback rules give it and by the place Flux's fallback rules give it; two objects of the same group and kind, which neither order separates by kind.

A failure MUST name every pair of kinds that the two orders put the opposite way round, with the library weight of each. It MUST tell the maintainer that the library's weight table is what changes to follow Flux, that the pin bump waits for that library release, and that the check and its list of kinds are not edited to make it pass.

#### Scenario: The pinned versions agree
- **WHEN** the test suite runs with the Flux version and the library version of `go.mod`
- **THEN** the check finds no pair of kinds in opposite order and passes

#### Scenario: A Flux bump moves a kind
- **WHEN** the Flux pin moves to a version whose reconcile order puts a Deployment before a Service, and the library still weighs a Service below a Deployment
- **THEN** the check fails without any edit to it
- **AND** the failure names the pair Deployment and Service with both weights

#### Scenario: A Flux bump adds a kind to its order
- **WHEN** the Flux pin moves to a version whose reconcile order newly names a kind, at a place that is the opposite of the weight the library's fallback gives that kind
- **THEN** the check fails and names the new kind

#### Scenario: A library bump moves a weight
- **WHEN** the library pin moves to a version that weighs a Deployment below a Service, and Flux still applies a Service before a Deployment
- **THEN** the check fails and names the pair

#### Scenario: Flux refines a tie
- **WHEN** the library weighs a ConfigMap and a Secret equally and Flux applies the ConfigMap first
- **THEN** the check does not fail for that pair
- **AND** the check reports how many such pairs it found

#### Scenario: A kind that only one order names
- **WHEN** a kind is named by the library's weight table and not by Flux's reconcile order, or the other way round, and no pair with it is in opposite order
- **THEN** the check does not fail
- **AND** a kind of Flux's order for which the check has no API group of its own is reported by name

#### Scenario: The failure says what to do
- **WHEN** the check fails
- **THEN** the message says that the library's weight table and the Flux list of the library's own guard change first, that the pin bump waits for that release, and that this check is not edited to pass

#### Scenario: A real staged apply follows the weights
- **WHEN** `Apply` applies a new set that holds a CustomResourceDefinition, a Namespace, a ClusterRole, a class kind, workloads, configuration, a webhook configuration and a custom resource to a real API server
- **THEN** every cluster definition is written before every class kind, and every class kind before every other object
- **AND** the library weight never decreases along the order of the writes

### Requirement: A forced recreate deletes exactly the object that was read
When an apply with `force` deletes an object to create it again, the DELETE MUST carry a precondition on the UID of the live object the apply read. The apply MUST NOT ask an ownership verdict for that delete. A DELETE the API server refuses on the precondition MUST fail the apply, and the object that now holds the name MUST NOT be deleted by that apply. The rule for PersistentVolumeClaims is checked first and is not changed. Source: owner decision of 2026-10-08 (the forced recreate carries the UID precondition only).

The resource manager's client MUST refuse a delete of a collection of objects, whatever the kind, because such a delete cannot name the object that was read.

#### Scenario: The recreated object is the one that was read
- **GIVEN** a live immutable ConfigMap and a resource set that changes its data
- **WHEN** the set is applied with `force` true
- **THEN** the DELETE request names the UID of the ConfigMap that was read
- **AND** the apply succeeds and the ConfigMap has a new UID and the new data

#### Scenario: An object replaced since the read is not deleted
- **GIVEN** the resource manager's client, and an object that was deleted and created again under the same name after it was read
- **WHEN** the client is asked to delete the object that was read
- **THEN** the delete is refused by the API server and the new object still exists
- **AND** the error says that the object was replaced

#### Scenario: A delete of a collection is refused
- **GIVEN** a resource manager built by the apply package
- **WHEN** its client is asked to delete all ConfigMaps of a namespace
- **THEN** the delete is refused and no ConfigMap is deleted

### Requirement: An apply is judged by the ownership verdict before its first write
Before the first write of a reconcile, the controller MUST read every object of the apply list and MUST ask the apply verdict of the library's `opm/k8s/ownership` package for it, with the instance's identity and with whether `status.inventory` lists the object (matched by group, kind, namespace and name). This holds for a ModuleInstance and a ModulePackage, for the apply of a changed render and for a restore. Source: 0012:D8:R1, 0012:D8:R2, 0012:D8:R5, 0012:D8:R8.

The read MUST be made by the client that applies: the impersonated ServiceAccount when one is effective, the controller's own identity otherwise. The controller MUST NOT set the verdict's install admission input.

The identity the verdict is asked with is the same for a ModuleInstance and a ModulePackage, and this paragraph is its one definition. The controller MUST ask once per object, with the instance's identity: the render's, or the recorded one when the render carries none. This holds when no identity is recorded yet and while an identity change is not settled. The controller MUST NOT ask with the earlier identity of an unsettled change (`status.previousInstanceUUID`): another record can render that identity, so an object that carries it is not proven to be the instance's own. The controller MUST NOT ask the apply verdict with an empty identity, and MUST NOT hand it the list the prune judges with, which holds the earlier identity and for a ModulePackage with no recorded identity ends with no identity. A reconcile that has no identity at all MUST fail as a failed apply with nothing written.

When the verdict refuses an object as `terminating`, `foreign-object` or `other-instance`, and the reconcile would write that object or the object exists and is not in `status.inventory`, the controller MUST NOT write anything in that reconcile: it applies no object, stores no identity and prunes nothing. The refused object MUST be left as it is.

When the verdict refuses an object as `adopted-elsewhere`, the controller MUST NOT apply that object and MUST apply the other objects.

An object that the verdict allows, that exists and that is not in `status.inventory` is taken in. The controller MUST NOT delete and create such an object again in the reconcile that takes it in, also when the object changes between the check and the apply: with `spec.rollout.forceConflicts`, when the API server refuses its update as immutable, the reconcile MUST fail before its first write, with `Ready=False` and reason `ApplyFailed`, and the message MUST name the object and the refused fields.

When the read of an object fails for a reason other than that the object does not exist, a reconcile that would write MUST NOT write anything and MUST fail as a failed apply. An object whose kind the API server does not serve yet, while a CustomResourceDefinition of the same apply list defines that kind, counts as an object that does not exist.

#### Scenario: A new object is created
- **GIVEN** a render that names a ConfigMap that does not exist
- **WHEN** the controller reconciles
- **THEN** the ConfigMap is created and listed in `status.inventory`

#### Scenario: A foreign object refuses the whole apply
- **GIVEN** a changed render of three objects, and a live ConfigMap with one of their names that carries no OPM label and is not in `status.inventory`
- **WHEN** the controller reconciles
- **THEN** none of the three objects is created or changed
- **AND** the ConfigMap keeps its content, labels and managed fields
- **AND** `status.inventory` and the applied digests keep their values

#### Scenario: Another instance's object refuses the apply
- **GIVEN** a render that names a Namespace that another instance created and that is not in this instance's inventory
- **WHEN** the controller reconciles
- **THEN** nothing is written and the Namespace keeps the other instance's UUID label

#### Scenario: The adopt annotation lets the instance take the object
- **GIVEN** the ConfigMap of the scenario above annotated `opmodel.dev/adopt` with this instance's UUID
- **WHEN** the controller reconciles
- **THEN** every object is applied, the ConfigMap carries this instance's labels and is listed in `status.inventory`

#### Scenario: An object being deleted refuses a changed render
- **GIVEN** an inventoried Deployment with a deletion timestamp and a changed render
- **WHEN** the controller reconciles
- **THEN** nothing is written

#### Scenario: An object adopted by another instance is not applied
- **GIVEN** an inventoried ConfigMap whose `opmodel.dev/adopt` annotation names another instance, and a changed render of that ConfigMap and a Deployment
- **WHEN** the controller reconciles
- **THEN** the Deployment is applied
- **AND** the ConfigMap keeps its content and is not deleted

#### Scenario: The earlier identity is not asked with while a change is not settled
- **GIVEN** an instance whose `status.previousInstanceUUID` is set, and a rendered PersistentVolumeClaim that exists outside the inventory with the earlier identity's UUID label
- **WHEN** the controller reconciles
- **THEN** nothing is written, the claim keeps the earlier identity's label, and `Ready` is `False` with reason `ApplyRefused`
- **AND** the message names the `opmodel.dev/adopt` annotation with the new identity

#### Scenario: An object adopted under the earlier identity is let go at once
- **GIVEN** an inventoried ConfigMap annotated `opmodel.dev/adopt` with the instance's identity, and a change of `spec.module.path` that gives the instance a new identity
- **WHEN** the controller reconciles and renders under the new identity
- **THEN** the ConfigMap is not written and leaves `status.inventory`, and the other objects are applied
- **AND** the ConfigMap is not deleted

#### Scenario: An unreadable object stops the apply
- **GIVEN** an effective ServiceAccount that may patch ConfigMaps and may not get them, and a changed render with a ConfigMap
- **WHEN** the controller reconciles
- **THEN** nothing is written and the object reports `Stalled=True` with reason `ImpersonationFailed`

#### Scenario: A custom resource of a CRD in the same render
- **GIVEN** a first render that holds a CustomResourceDefinition and a custom resource of its kind
- **WHEN** the controller reconciles
- **THEN** the read of the custom resource counts as "does not exist" and both objects are applied

#### Scenario: A package with no recorded identity lets an adopted object go
- **GIVEN** a ModulePackage with an empty `status.instanceUUID`, and an inventoried ConfigMap whose `opmodel.dev/adopt` annotation names another instance
- **WHEN** the controller reconciles and renders
- **THEN** the ConfigMap is not written and leaves `status.inventory`
- **AND** the verdict was asked with the render's identity, never with an empty one

#### Scenario: An inventoried object with another instance's UUID label is applied
- **GIVEN** two Ready instances that both list Namespace `shared` in `status.inventory`, where the Namespace carries the first instance's UUID label and no adopt annotation
- **WHEN** the second instance reconciles with a changed render
- **THEN** its render is applied, the Namespace included, and `Ready` is `True`
- **AND** no `ApplyRefused` condition or event appears on either instance

#### Scenario: An inventoried object without OPM labels is applied
- **GIVEN** a Ready instance whose inventoried ConfigMap lost every OPM label
- **WHEN** the instance reconciles with a changed render
- **THEN** the ConfigMap is applied and carries the instance's labels again

#### Scenario: A taken-in object is not recreated
- **GIVEN** an instance with `spec.rollout.forceConflicts: true`, and a live Service outside its inventory that is annotated `opmodel.dev/adopt` with the instance's UUID and whose immutable `spec.clusterIP` differs from the render
- **WHEN** the controller reconciles
- **THEN** nothing is written, the Service keeps its UID, and `Ready` is `False` with reason `ApplyFailed` and a message that names the Service
