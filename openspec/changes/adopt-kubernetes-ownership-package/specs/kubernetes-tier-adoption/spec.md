## ADDED Requirements

### Requirement: The operator decides deletes only through the library's ownership package
Every delete of a cluster object that the operator makes on behalf of a ModuleInstance or a ModulePackage MUST follow a delete verdict of the library's `opm/k8s/ownership` package on a live read of that object. The operator MUST keep no rule of its own that compares the managed-by label, the UUID label or the adopt annotation to decide a delete, and no list of its own of kinds that are never deleted. The operator MUST NOT set the verdict's install admission input. Source: 0012:D4:R1.

A test MUST list the places in the operator's code that may send a DELETE for a cluster object, and MUST fail when a DELETE is sent from any other place.

#### Scenario: No local copy
- **WHEN** the operator's non-test code is searched for a comparison of the managed-by label or the UUID label that decides a delete
- **THEN** none is found outside calls into `opm/k8s/ownership`

#### Scenario: A new delete outside the verdict fails the tests
- **GIVEN** a change that adds a call that deletes a cluster object in a file the test does not list
- **WHEN** the unit tests run
- **THEN** the call-site test fails and names the file
