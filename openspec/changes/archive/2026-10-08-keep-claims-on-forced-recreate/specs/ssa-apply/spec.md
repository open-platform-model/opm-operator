## ADDED Requirements

### Requirement: A forced recreate keeps PersistentVolumeClaims unless data deletion is allowed
The apply MUST take, beside `force`, an option that allows the deletion of data, and the option MUST be off at its zero value. When `force` is true and the option is off, the apply MUST NOT delete a PersistentVolumeClaim of the core API group. Before it applies or deletes any object, it MUST check every such claim of the resource set that exists in the cluster. When the API server refuses the update of one (the error the forced recreate would answer with a delete), the apply MUST return an error that names the claim's namespace and name, the refused field or fields as the API server reports them, and the API server's message. In that case no object of the resource set is applied, created or deleted.

When the option is on, a forced recreate MUST delete and recreate a claim as it does any other object. When `force` is false, the option has no effect. The forced recreate of every other kind MUST NOT change.

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
