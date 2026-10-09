## ADDED Requirements

### Requirement: The IdentityChangeUnsettled reason
The status package SHALL define the reason constant `IdentityChangeUnsettled`. A ModuleInstance or ModulePackage reconcile that refuses a second change of the instance identity while the first is not settled MUST set `Ready=False` and `Stalled=True` with that reason. The message MUST say that an earlier change of the instance identity is not finished, and MUST name the way out: restore the earlier module path, wait until the object is Ready, then change it again. The message MUST NOT carry an enhancement reference.

#### Scenario: The reason on a refused second change
- **GIVEN** a ModuleInstance whose identity change is not settled and whose render carries a third identity
- **WHEN** the controller reconciles
- **THEN** `Ready` is `False` with reason `IdentityChangeUnsettled` and `Stalled` is `True`
- **AND** the message tells the user to restore the earlier module path
