## ADDED Requirements

### Requirement: The operator decides every apply only through the library's ownership package
Every write of a cluster object that the operator makes on behalf of a ModuleInstance or a ModulePackage MUST follow an apply verdict of the library's `opm/k8s/ownership` package on a live read of that object. The operator MUST declare no ownership rule of its own for an apply: it compares no managed-by label, no UUID label and no adopt annotation itself. The operator MUST NOT set, change or remove the `opmodel.dev/adopt` annotation on any object. Source: 0012:D4:R2, 0012:D8:R6.

A test MUST keep the list of places that may write a cluster object closed: the staged apply, the dry runs of drift detection and of the claim check, and the status and finalizer patches of the operator's own kinds. It MUST find a write by the type of the receiver, as the test of the delete call sites does, and a second test MUST show that the matcher finds a write of each form the client and the resource manager offer.

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
