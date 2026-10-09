## ADDED Requirements

### Requirement: The ApplyRefused reason
The status package SHALL define the reason constant `ApplyRefused`. A ModuleInstance or ModulePackage reconcile that the apply verdict refuses MUST set `Ready=False` with that reason and MUST NOT set `Stalled`. The message MUST state how many objects were refused and that nothing was applied, and MUST carry, for each refused object, the message the library words for the refusal, unchanged (at most ten objects, and fewer when the messages would take the text past 1024 characters; then the number of the rest). The message MUST NOT carry an enhancement reference.

#### Scenario: The reason on a refused apply
- **GIVEN** a ModuleInstance whose render names a live ConfigMap `team-a/settings` that OPM does not manage
- **WHEN** the controller reconciles
- **THEN** `Ready` is `False` with reason `ApplyRefused` and `Stalled` is absent
- **AND** the message contains the library's message for `ConfigMap/team-a/settings`, which names the `opmodel.dev/adopt` annotation and the instance's UUID

#### Scenario: A let-go object does not set the reason
- **GIVEN** a reconcile whose only refused object is adopted by another instance
- **WHEN** the reconcile completes
- **THEN** `Ready` is `True`

### Requirement: The Ready message counts the objects adopted by another instance
When a ModuleInstance or ModulePackage reconcile renders, ends `Ready=True` (after an apply, a restore or a no-op) and found one or more rendered objects that the apply verdict refuses as `adopted-elsewhere`, the message of the `Ready` condition MUST state how many rendered objects are adopted by another instance and are not applied. When it found none, the message MUST NOT mention such objects. A reconcile that skips its render MUST leave the message as the last render wrote it. The message MUST NOT carry an enhancement reference. Source: 0012:D8:R8.

#### Scenario: The count after a let-go
- **GIVEN** a Ready instance, two of whose rendered objects are annotated for another instance
- **WHEN** a reconcile renders with unchanged digests
- **THEN** `Ready` is `True` and its message states that 2 rendered objects are adopted by another instance and not applied

#### Scenario: The count goes away
- **GIVEN** the instance above
- **WHEN** both objects are annotated back for the instance and a reconcile renders
- **THEN** `Ready` is `True` and its message no longer mentions adopted objects
