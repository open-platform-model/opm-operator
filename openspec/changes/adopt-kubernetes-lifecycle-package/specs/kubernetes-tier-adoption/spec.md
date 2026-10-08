## ADDED Requirements

### Requirement: The operator deletes inventory objects only as the library's deletion plan names them
Every delete of a cluster object that the operator makes in a prune of stale resources or in a deletion cleanup, for a ModuleInstance or a ModulePackage, MUST be an action the library's `opm/k8s/lifecycle` transition named, performed with the propagation policy and the precondition of that action. The operator MUST build each plan with the library's constructor and MUST NOT order, filter by ownership, or set a propagation policy with code of its own. Source: 0012:D4:R1.

One function of the operator MUST perform the plan's reads and deletes. The closed list of places that may delete a cluster object MUST hold that function and the forced recreate, and nothing else.

The delete inside a forced recreate stays outside the plan: it is part of an apply, it asks no verdict, and it MUST carry the UID precondition (capability `ssa-apply`).

#### Scenario: The plan's order and propagation reach the API server unchanged
- **GIVEN** a plan over a ConfigMap and a Deployment
- **WHEN** the operator performs it
- **THEN** the DELETE requests arrive in the plan's order, each with the action's propagation policy and UID precondition

#### Scenario: A delete outside the runner fails the tests
- **GIVEN** a change that adds a call that deletes a cluster object outside the plan runner and the forced recreate
- **WHEN** the unit tests run
- **THEN** the call-site test fails and names the file

#### Scenario: The forced recreate is not a plan step
- **GIVEN** an apply that is allowed to recreate objects, and an object the API server refuses to update
- **WHEN** the object is deleted and created again
- **THEN** the DELETE carries the UID precondition and no deletion plan is built for it
