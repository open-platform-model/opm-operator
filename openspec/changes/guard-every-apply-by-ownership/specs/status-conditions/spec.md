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
