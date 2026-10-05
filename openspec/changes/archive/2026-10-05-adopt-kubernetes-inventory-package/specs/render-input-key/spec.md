## MODIFIED Requirements

### Requirement: An operator or library upgrade renders every object once

Because the operator and library versions are key parts, the first reconcile of each object after either version changes SHALL render. The rule rests on every operator release changing `version.Version`; a development image built without a version bump keeps its key. A change to how the operator computes a stored digest (`lastApplied*` digests, `status.inventory.digest`) SHALL ship with an operator version change or a library version change, so that render finds the stored digests out of date and applies once. Starting the same operator and library again (a restart) SHALL NOT by itself make an object render while its key matches and the interval has not passed.

#### Scenario: A newer operator renders and applies once

- **WHEN** an instance's key was recorded by operator `v1.0.0-beta.9`, the running operator is `v1.0.0-beta.10`, and the render digest the new operator computes differs from `lastAppliedRenderDigest`
- **THEN** the reconcile renders and applies
- **AND** `status.lastAppliedInputs` then carries the new operator's key

#### Scenario: A restart does not render unchanged objects

- **WHEN** the operator restarts, the platform store is still empty, and an instance's key matches and was recorded 10 minutes ago
- **THEN** the instance is not rendered and stays `Ready=True`, without passing through `PlatformNotReady`

#### Scenario: The library inventory digests apply once and then converge

- **WHEN** an instance or package whose `status.inventory.digest` and `lastAppliedRenderDigest` were written in the earlier operator's encoding, with a key recorded by operator `v1.0.0-test`, is reconciled by operator `v1.0.1-test` that computes them with `opm/k8s/inventory`
- **THEN** the reconcile renders once and applies once, and the stored digests become the library's digests of the same entries and render
- **AND** the next reconcile under `v1.0.1-test` skips the render, and a render after the interval ends `NoOp` without a second apply

#### Scenario: A key recorded by the same version keeps the old digests until the interval

- **WHEN** an instance holds digests in the earlier encoding and a key recorded by the running operator version
- **THEN** the reconcile skips its render, because the key holds no digest
- **AND** the first render after the interval applies once and records the new digests
