## ADDED Requirements

### Requirement: Deletion-cleanup SA-missing stalls with distinct reason
When the deletion cleanup path attempts to build an impersonated client and the target ServiceAccount does not exist (apiserver returns NotFound for the SA lookup), the controller MUST:

- NOT silently fall back to the controller's own client.
- Stall the release with `Ready=False` and condition reason `DeletionSAMissing` (distinct from the generic `ImpersonationFailed` used on the apply path).
- Populate the condition message with the SA's namespaced name and list the operator recovery options (restore SA, set `spec.prune=false`, or set the orphan annotation).
- Retain the finalizer.
- Emit a `Warning` event with reason `DeletionSAMissing` on the Ready transition (not on every requeue).
- Requeue on the stalled-recheck interval, not the tight transient backoff.

This behavior MUST apply symmetrically to both `ModuleRelease` and `Release` deletion paths.

#### Scenario: SA deleted before finalizer can prune
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=hello-applier`, `spec.prune=true`, and a non-empty inventory
- **AND** the ServiceAccount `hello-applier` has been deleted from the release's namespace
- **WHEN** the ModuleRelease is deleted and the finalizer runs deletion cleanup
- **THEN** the impersonated client build fails with SA-NotFound
- **AND** the release's Ready condition becomes False with reason `DeletionSAMissing`
- **AND** the condition message names `default/hello-applier` and lists recovery options
- **AND** a Warning event is emitted with reason `DeletionSAMissing`
- **AND** the finalizer is NOT removed
- **AND** the controller client is NOT used as a fallback for prune

#### Scenario: Other impersonation errors keep existing behavior
- **GIVEN** a ModuleRelease with `spec.serviceAccountName=deploy-sa` and the controller lacks `impersonate` RBAC
- **WHEN** the ModuleRelease is deleted and the finalizer runs deletion cleanup
- **THEN** the release stalls with reason `ImpersonationFailed` (the existing generic reason)
- **AND** the reason is NOT `DeletionSAMissing`
- **AND** the orphan annotation has no effect on this case

### Requirement: Orphan-exit annotation removes finalizer on SA-missing
When a release is in the `DeletionSAMissing` stall state AND the annotation `opm.dev/force-delete-orphan` is set to `"true"` on the release, the controller MUST:

- Skip prune entirely.
- Remove the finalizer.
- Emit a `Warning` event with reason `OrphanedOnDeletion` naming the number of inventory entries left behind.
- Clear `status.inventory` in the final status patch so the last-observed state does not claim ownership of resources the controller is abandoning.

The annotation MUST only take effect when the deletion cleanup impersonation failure is SA-NotFound. For any other impersonation error (transient, RBAC-denied, etc.), the annotation MUST be ignored and the existing stall behavior preserved.

Any annotation value other than the literal string `"true"` MUST be treated as absent.

#### Scenario: Orphan annotation releases a stuck deletion
- **GIVEN** a ModuleRelease stalled with reason `DeletionSAMissing` and `status.inventory` listing 3 entries
- **WHEN** an operator patches the release with annotation `opm.dev/force-delete-orphan=true`
- **AND** the next reconcile runs
- **THEN** prune is not attempted
- **AND** a Warning event is emitted with reason `OrphanedOnDeletion` and message referencing 3 orphaned entries
- **AND** the finalizer is removed
- **AND** the release object is garbage-collected by the apiserver
- **AND** the 3 previously-managed resources remain in the cluster

#### Scenario: Orphan annotation ignored for non-SA-missing stall
- **GIVEN** a ModuleRelease stalled with reason `ImpersonationFailed` (RBAC-denied impersonate verb, not SA-missing)
- **AND** the annotation `opm.dev/force-delete-orphan=true` is set
- **WHEN** the next reconcile runs
- **THEN** the annotation has no effect
- **AND** the release remains stalled with `ImpersonationFailed`
- **AND** the finalizer is NOT removed

#### Scenario: Annotation value other than "true" is treated as absent
- **GIVEN** a ModuleRelease stalled with reason `DeletionSAMissing`
- **AND** the annotation `opm.dev/force-delete-orphan=yes` is set (value is not the literal `"true"`)
- **WHEN** the next reconcile runs
- **THEN** the annotation is ignored
- **AND** the release remains stalled with `DeletionSAMissing`
- **AND** the finalizer is NOT removed
