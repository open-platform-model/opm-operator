## ADDED Requirements

### Requirement: The instance identities are stored before the first write of an apply
A ModuleInstance and a ModulePackage MUST carry in `status.instanceUUID` the identity of the instance the operator last rendered and began to apply, and in the optional field `status.previousInstanceUUID` the identity it had before, for as long as a change of identity is not settled. Source: owner decision of 2026-10-08 (the status holds the earlier and the new identity until the prune of that reconcile succeeded; then the earlier one is cleared).

When a render's identity differs from `status.instanceUUID`, the reconciler MUST store the new state in the object's status before it applies any object, and MUST NOT apply when that write fails:

- `status.instanceUUID` empty: it becomes the render's identity and `status.previousInstanceUUID` stays empty.
- `status.instanceUUID` is `A`, `status.previousInstanceUUID` is empty and the render carries `B`: `status.instanceUUID` becomes `B` and `status.previousInstanceUUID` becomes `A`.
- `status.instanceUUID` is `B`, `status.previousInstanceUUID` is `A` and the render carries `A`: the two values swap.

`status.previousInstanceUUID` MUST be cleared in the status commit of a reconcile whose apply and prune succeeded, together with the new inventory, and MUST NOT be cleared by any other outcome: a failed or refused apply, a failed prune, a reconcile that applies nothing, or a recovered panic. A reconcile that applies nothing MUST write `status.instanceUUID` when it is empty and MUST leave both fields alone otherwise.

While `status.previousInstanceUUID` is set, or a render is about to set it, a reconcile MUST NOT end as one that applies nothing, also when the render equals what was last applied: it MUST apply and prune, so that the objects an unsettled apply relabelled are relabelled back and the change is settled.

#### Scenario: First reconcile records the identity
- **GIVEN** a new ModuleInstance whose render carries identity `A`
- **WHEN** the reconcile applies with success
- **THEN** `status.instanceUUID` is `A` and `status.previousInstanceUUID` is empty

#### Scenario: Both identities are stored before the apply
- **GIVEN** a ModuleInstance with `status.instanceUUID` `A` whose `spec.module.path` changes, so that its render carries identity `B`
- **WHEN** the apply fails
- **THEN** the stored object has `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`
- **AND** `status.inventory` is unchanged

#### Scenario: A refused apply keeps both identities
- **GIVEN** the same change of identity, and an apply that succeeds while the reconcile is refused afterwards because dependents remain
- **WHEN** the reconcile ends
- **THEN** `status.instanceUUID` is `B` and `status.previousInstanceUUID` is `A`

#### Scenario: A panic keeps both identities
- **GIVEN** the same change of identity, and a reconcile that panics after the identities were stored
- **WHEN** the panic was recorded as a failed attempt
- **THEN** `status.instanceUUID` is `B` and `status.previousInstanceUUID` is `A`

#### Scenario: Full success settles the change
- **GIVEN** a ModuleInstance with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`
- **WHEN** a reconcile applies and prunes with success
- **THEN** `status.previousInstanceUUID` is empty and `status.instanceUUID` is `B`

#### Scenario: Going back before the change is settled
- **GIVEN** a ModuleInstance with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`, whose `spec.module.path` is set back so that its render carries `A`
- **WHEN** the apply starts
- **THEN** `status.instanceUUID` is `A` and `status.previousInstanceUUID` is `B`

#### Scenario: Going back renders what was last applied
- **GIVEN** a ModuleInstance whose last successful apply carried identity `A`, with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A` after an apply of `B` whose prune failed, and whose `spec.module.path` is set back so that its render equals what was last applied
- **WHEN** the controller reconciles
- **THEN** the render is applied, and the objects that carry `B` carry `A` again
- **AND** after the reconcile `status.instanceUUID` is `A`, `status.previousInstanceUUID` is empty and `Ready` is `True`

#### Scenario: An object without the field gains it without an apply
- **GIVEN** a ModuleInstance with an inventory and an empty `status.instanceUUID`, whose render equals what was applied
- **WHEN** the controller reconciles and applies nothing
- **THEN** `status.instanceUUID` holds the render's identity and `status.previousInstanceUUID` is empty

#### Scenario: The identities cannot be stored
- **GIVEN** a change of identity, and an API server that refuses the status write
- **WHEN** the controller reconciles
- **THEN** no object of the render is applied and the reconcile is retried

### Requirement: A second identity change waits until the first is settled
While `status.previousInstanceUUID` is set, a render whose identity is neither `status.instanceUUID` nor `status.previousInstanceUUID` MUST NOT be applied, and nothing MUST be pruned for it. The reconcile MUST report `Ready=False` and `Stalled=True` with reason `IdentityChangeUnsettled`, MUST leave both identity fields and the inventory as they are, and MUST recheck on the stalled interval. The deletion cleanup is not affected.

#### Scenario: A third identity is refused
- **GIVEN** a ModuleInstance with `status.instanceUUID` `B` and `status.previousInstanceUUID` `A`, whose `spec.module.path` changes again so that its render carries identity `C`
- **WHEN** the controller reconciles
- **THEN** no object is applied or deleted
- **AND** `Ready` is `False` and `Stalled` is `True` with reason `IdentityChangeUnsettled`
- **AND** `status.instanceUUID` is `B` and `status.previousInstanceUUID` is `A`

#### Scenario: The refusal clears when the earlier path is restored
- **GIVEN** that ModuleInstance
- **WHEN** `spec.module.path` is set back so that the render carries `B`, and the reconcile applies and prunes with success
- **THEN** `Ready` is `True` and `status.previousInstanceUUID` is empty

#### Scenario: Deletion still works
- **GIVEN** that ModuleInstance with reason `IdentityChangeUnsettled` and `spec.prune=true`
- **WHEN** it is deleted
- **THEN** the cleanup deletes its inventory objects that carry `A` or `B` and removes the finalizer
