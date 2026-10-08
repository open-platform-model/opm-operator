## ADDED Requirements

### Requirement: The prune runs the library's deletion plan
The prune of stale resources MUST delete a stale object only when the library's deletion transition (`opm/k8s/lifecycle`) names that delete as the next action. Source: 0012:D4:R1.

The prune MUST send each delete in the order the plan gives, descending kind weight, and MUST add no ordering of its own. Each delete MUST carry the propagation policy the action names, Foreground, and the action's UID precondition.

With more than one identity to judge with, the prune MUST run one plan per identity, in the order the identities are asked today: the entries a plan skips because the object belongs to, or is being adopted by, another instance form the plan of the next identity.

A PersistentVolumeClaim that `spec.dataPolicy` keeps MUST NOT be an entry of any plan; it is still read and judged, so that it is reported as kept, left behind or gone as before.

The prune MUST NOT wait for a deleted object to disappear. A stale entry whose delete the API server accepted leaves `status.inventory`, as before.

#### Scenario: Stale objects are deleted in descending kind weight
- **GIVEN** a stale set that lists a ConfigMap before a Deployment
- **WHEN** the controller prunes the stale set
- **THEN** the DELETE of the Deployment is sent before the DELETE of the ConfigMap

#### Scenario: Stale deletes use Foreground propagation
- **GIVEN** a stale Deployment owned by the instance
- **WHEN** the controller prunes it
- **THEN** the DELETE request carries the propagation policy `Foreground`

#### Scenario: The reconcile does not wait for a stale object
- **GIVEN** a stale Deployment whose delete is accepted and which still exists with a `deletionTimestamp`
- **WHEN** the reconcile ends
- **THEN** `Ready` is True and `status.inventory.entries` does not list the Deployment

#### Scenario: A kept stale claim is not in the plan
- **GIVEN** a ModuleInstance with no `spec.dataPolicy` and a stale PersistentVolumeClaim of its own
- **WHEN** the controller prunes the stale set
- **THEN** no DELETE is sent for the claim and it is reported as kept
