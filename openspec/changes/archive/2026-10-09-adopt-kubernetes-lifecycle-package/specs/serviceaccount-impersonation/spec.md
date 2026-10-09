## MODIFIED Requirements

### Requirement: Deletion-cleanup SA-missing stalls with distinct reason
When the deletion cleanup path attempts to build an impersonated client and the target ServiceAccount does not exist (apiserver returns NotFound for the SA lookup), the controller MUST:

- NOT silently fall back to the controller's own client.
- Stall the release with `Ready=False` and condition reason `DeletionSAMissing` (distinct from the generic `ImpersonationFailed` used on the apply path).
- Populate the condition message with the SA's namespaced name and list the operator recovery options (restore SA, set `spec.prune=false`, or set the orphan annotation).
- Retain the finalizer.
- Emit a `Warning` event with reason `DeletionSAMissing` on the Ready transition (not on every requeue).
- Requeue on the stalled-recheck interval, not the tight transient backoff.

This behavior MUST apply symmetrically to both `ModuleRelease` and `Release` deletion paths.

Two cases are not this stall, because the missing ServiceAccount has nothing left to do (capability `finalizer-and-deletion`): an inventory that holds nothing the cleanup would delete (it is empty, or holds only PersistentVolumeClaims that `spec.dataPolicy` keeps), and a cleanup that already sent every delete and only waits for the objects to be gone. In both the finalizer is removed, the controller's own client is still never used, and a `Warning` event with reason `DeletionUnconfirmed` says what could not be read.

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

#### Scenario: Only kept claims and no ServiceAccount
- **GIVEN** a ModuleInstance with `spec.prune=true` and no `spec.dataPolicy` being deleted, whose inventory holds only PersistentVolumeClaims, and whose ServiceAccount does not exist
- **WHEN** the controller reconciles it
- **THEN** no object is read or deleted and the finalizer is removed
- **AND** the reason `DeletionSAMissing` is never set
- **AND** a `Warning` event with reason `DeletionUnconfirmed` says that the claims were left in place without a read

#### Scenario: ServiceAccount removed while the cleanup waits
- **GIVEN** a ModuleInstance being deleted whose `Ready` condition carries the reason `DeletionInProgress`
- **WHEN** its ServiceAccount is deleted and the controller reconciles the instance
- **THEN** the finalizer is removed and the reason `DeletionSAMissing` is never set
