## ADDED Requirements

### Requirement: Events emitted while a deletion waits
The controller MUST emit one Normal event with reason `DeletionInProgress` and action `Delete` when a deletion cleanup starts to wait for deleted objects, and one Warning event with reason `DeletionBlocked` and action `Delete` when the wait becomes blocked. Each event MUST carry the message of the condition. The controller MUST NOT repeat either event at a later reconcile while `Ready` already carries that reason.

#### Scenario: One event when the wait starts
- **GIVEN** a deleting ModuleInstance whose cleanup deleted a Deployment that still exists
- **WHEN** three reconciles run while it still exists
- **THEN** exactly one Normal event with reason `DeletionInProgress` was emitted

#### Scenario: One warning when the wait is blocked
- **GIVEN** a deleting ModuleInstance that has reason `DeletionInProgress` and whose Deployment passes 10 minutes of termination
- **WHEN** the next reconciles run
- **THEN** exactly one Warning event with reason `DeletionBlocked` was emitted, and it names the Deployment
