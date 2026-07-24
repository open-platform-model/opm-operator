# Delta: module-instance-ownership (envtest-coverage-batch)

No behavior change. The ownership-marker requirement gains the reconcile-side scenario for the operator-managed default (absent owner) that its prose already states — the contract enhancement 0006 D24's skew model leans on.

## MODIFIED Requirements

### Requirement: ModuleInstance carries an ownership marker

`ModuleInstanceSpec` SHALL provide an optional `owner` field of a typed enum with exactly two valid values, `cli` and `operator`, serialized as `owner` with `omitempty`. The field SHALL NOT define a CRD-level default. The API SHALL define exported constants for both values. An absent or empty `owner` SHALL be treated by the controller as operator-managed (see the skip requirement).

#### Scenario: Field accepts the two enum values

- **WHEN** a `ModuleInstance` is created with `spec.owner` set to `cli` or to `operator`
- **THEN** the API server accepts it
- **AND** a value other than `cli` or `operator` is rejected by enum validation

#### Scenario: Field is optional with no default

- **WHEN** a `ModuleInstance` is created with no `spec.owner`
- **THEN** the API server accepts it
- **AND** the stored object's `spec.owner` remains empty (no value is defaulted in)

#### Scenario: Empty owner is reconciled as operator-managed

- **WHEN** a `ModuleInstance` with no `spec.owner` is reconciled
- **THEN** the controller SHALL register the cleanup finalizer and perform a normal render/apply reconcile (no owner-skip, no `ManagedExternally` acknowledgement)

#### Scenario: Unknown owner values cannot reach the reconciler

- **WHEN** a client attempts to create a `ModuleInstance` with `spec.owner: "future-actor"`
- **THEN** the API server SHALL reject it via enum validation, so the controller never observes an unknown owner value
