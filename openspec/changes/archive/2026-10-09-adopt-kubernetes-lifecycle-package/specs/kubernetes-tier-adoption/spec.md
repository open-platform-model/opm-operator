## MODIFIED Requirements

### Requirement: The operator decides prune and cleanup deletes only through the library's ownership package
Every delete of a cluster object that the operator makes in a prune of stale resources or in a deletion cleanup, for a ModuleInstance or a ModulePackage, MUST follow a delete verdict of the library's `opm/k8s/ownership` package on a live read of that object. The operator MUST take the kinds that are never deleted from the same package. The operator MUST NOT set the verdict's install admission input. Source: 0012:D4:R1.

Each such delete MUST be an action the library's `opm/k8s/lifecycle` transition named, performed with the propagation policy and the precondition of that action. The operator MUST build each plan with the library's constructor and MUST NOT order the entries or set a propagation policy with code of its own. One function of the operator MUST perform the plan's reads and deletes.

The one other delete the operator makes, the delete inside a forced recreate, asks no verdict and MUST carry the UID precondition (capability `ssa-apply`).

A test MUST keep the list of places that may delete a cluster object closed: the function that performs the plan, and the forced recreate. It MUST find a delete by the type of the receiver, a Kubernetes client, and not by the method name alone, so that a method of the same name on another type is not counted, and a second test MUST show that the matcher finds a delete of each form the client offers.

#### Scenario: The prune's outcome is the verdict's
- **GIVEN** one stale object for each answer of the library's delete verdict
- **WHEN** the prune runs
- **THEN** it deletes exactly the objects for which the verdict says proceed, and names each other object with the verdict's reason

#### Scenario: A new delete outside the listed places fails the tests
- **GIVEN** a change that adds a call that deletes a cluster object in a file the test does not list
- **WHEN** the unit tests run
- **THEN** the call-site test fails and names the file

#### Scenario: A method of the same name on another type is not a delete
- **GIVEN** the operator's calls that remove a status condition
- **WHEN** the call-site test runs
- **THEN** it does not count them

#### Scenario: The plan's order and propagation reach the API server unchanged
- **GIVEN** a plan over a ConfigMap and a Deployment
- **WHEN** the operator performs it
- **THEN** the DELETE requests arrive in the plan's order, each with the action's propagation policy and UID precondition

#### Scenario: The forced recreate is not a plan step
- **GIVEN** an apply that is allowed to recreate objects, and an object the API server refuses to update
- **WHEN** the object is deleted and created again
- **THEN** the DELETE carries the UID precondition and no deletion plan is built for it
