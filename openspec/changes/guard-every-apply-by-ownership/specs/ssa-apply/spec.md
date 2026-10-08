## ADDED Requirements

### Requirement: An apply is judged by the ownership verdict before its first write
Before the first write of a reconcile, the controller MUST read every object of the apply list and MUST ask the apply verdict of the library's `opm/k8s/ownership` package for it, with the render's instance identity and with whether `status.inventory` lists the object (matched by group, kind, namespace and name). This holds for a ModuleInstance and a ModulePackage, for the apply of a changed render and for a restore. Source: 0012:D8:R1, 0012:D8:R2, 0012:D8:R5, 0012:D8:R8.

The read MUST be made by the client that applies: the impersonated ServiceAccount when one is effective, the controller's own identity otherwise. The controller MUST NOT set the verdict's install admission input.

While an identity change is not settled, the controller MUST ask again with the earlier identity when the first answer is `foreign-object`, `other-instance` or `adopted-elsewhere`, and the object is allowed when either answer allows it. Otherwise the reason and the message of the first answer are the ones reported. A `terminating` answer is never asked again.

When the verdict refuses an object as `terminating`, `foreign-object` or `other-instance`, and the reconcile would write that object or the object exists and is not in `status.inventory`, the controller MUST NOT write anything in that reconcile: it applies no object, stores no identity and prunes nothing. The refused object MUST be left as it is.

When the verdict refuses an object as `adopted-elsewhere`, the controller MUST NOT apply that object and MUST apply the other objects.

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

#### Scenario: The earlier identity is accepted while a change is not settled
- **GIVEN** an instance whose `status.previousInstanceUUID` is set, and a rendered PersistentVolumeClaim that exists outside the inventory with the earlier identity's UUID label
- **WHEN** the controller reconciles
- **THEN** the claim is applied and carries the new identity's label

#### Scenario: An unreadable object stops the apply
- **GIVEN** an effective ServiceAccount that may patch ConfigMaps and may not get them, and a changed render with a ConfigMap
- **WHEN** the controller reconciles
- **THEN** nothing is written and the object reports `Stalled=True` with reason `ImpersonationFailed`

#### Scenario: A custom resource of a CRD in the same render
- **GIVEN** a first render that holds a CustomResourceDefinition and a custom resource of its kind
- **WHEN** the controller reconciles
- **THEN** the read of the custom resource counts as "does not exist" and both objects are applied
