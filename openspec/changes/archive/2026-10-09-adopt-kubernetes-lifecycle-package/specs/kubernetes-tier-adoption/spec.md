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

### Requirement: The operator decides every apply only through the library's ownership package
Every write of a cluster object that the operator makes on behalf of a ModuleInstance or a ModulePackage MUST follow an apply verdict of the library's `opm/k8s/ownership` package on a live read of that object. The operator MUST declare no ownership rule of its own for an apply: it compares no managed-by label, no UUID label and no adopt annotation itself. The operator MUST NOT set, change or remove the `opmodel.dev/adopt` annotation on any object. Source: 0012:D4:R2, 0012:D8:R6.

A test MUST keep the list of places that may write a cluster object closed: the staged apply, the dry runs of drift detection and of the claim check, the status and finalizer patches of the operator's own kinds, and the access review a waiting deletion cleanup asks (a SelfSubjectAccessReview of the controller's own right to impersonate a ServiceAccount, capability `finalizer-and-deletion`). The access review is in the list because it is sent with a create call; the API server answers it in the response and stores no object. It MUST find a write by the type of the receiver, as the test of the delete call sites does, and a second test MUST show that the matcher finds a write of each form the client and the resource manager offer.

#### Scenario: The apply's outcome is the verdict's
- **GIVEN** one rendered object for each answer of the library's apply verdict
- **WHEN** the guard runs
- **THEN** it allows exactly the objects for which the verdict says apply, and names each other object with the verdict's reason and message

#### Scenario: A new write outside the listed places fails the tests
- **GIVEN** a change that adds a call that creates, updates, patches or applies a cluster object in a file the test does not list
- **WHEN** the unit tests run
- **THEN** the call-site test fails and names the file

#### Scenario: No write of the adopt annotation
- **WHEN** the non-test Go files of the operator are parsed by a unit test
- **THEN** none names the library's adopt annotation key, so the annotation is read only inside the library's verdicts and is never written
