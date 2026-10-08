## MODIFIED Requirements

### Requirement: The claimed artifact must be a catalog

Acceptance SHALL acquire the artifact named by `spec.catalog` at `spec.version` and SHALL refuse the claim unless that artifact is a `#Catalog` (enhancement 0015 D10). The refusal SHALL be structural (the kind gate the acquisition applies) rather than a rule this repo maintains. An artifact that cannot be resolved at all SHALL be refused distinguishably from one that resolves and is the wrong kind, since the first is a registry or coordinate problem and the second is an authoring one. One case is not a refusal: an accepted claim whose acquisition fails with a transient registry failure keeps its verdict, as the requirement "An accepted claim keeps its verdict through a transient registry failure" states.

#### Scenario: A module artifact is refused

- **WHEN** `spec.catalog` names an artifact whose kind is `Module`
- **THEN** the claim is refused, naming the kind found

#### Scenario: An unresolvable coordinate is refused distinguishably

- **WHEN** `spec.catalog` at `spec.version` resolves to nothing in the registry
- **THEN** the claim is refused with a reason distinct from the wrong-kind refusal, naming the coordinate

## ADDED Requirements

### Requirement: An accepted claim keeps its verdict through a transient registry failure

When the acquisition of the claimed catalog fails with a transient registry failure, a claim whose `status.accepted` is true and whose `status.observedGeneration` equals its `metadata.generation` SHALL keep its verdict: `status.accepted`, `status.active`, the `Active` condition and a `Ready` condition that reports the acceptance SHALL stay as they were. A `Ready` condition that is not `True` reports no verdict (an earlier reconcile deferred it, as the first reconcile after an operator restart does); it SHALL become `Unknown` with reason `CatalogUnresolved`, so it names the current cause. A transient registry failure is the one the library classifies as transient: the registry gave no HTTP response, or it answered with a 5xx status. The operator SHALL NOT classify by message text.

The operator SHALL report the failure without changing the verdict: the claim SHALL carry `Reconciling=True` with reason `CatalogUnresolved`, and the operator SHALL emit one Warning event that carries the registry error when the claim enters this state. The condition message SHALL NOT change between attempts, so repeated attempts write no status and emit no further event.

The operator SHALL retry the acquisition with an exponential backoff that starts at 5 seconds, doubles, and is capped at 5 minutes, with jitter. The backoff SHALL be measured from the time the claim entered this state, so an operator restart does not reset it. When an attempt gets an answer, the claim SHALL be judged on that answer and SHALL no longer report `Reconciling` with reason `CatalogUnresolved`: an acceptance or a refusal removes the condition, and a deferred verdict replaces its reason.

Every other acquisition failure SHALL refuse the claim, accepted or not: a catalog or version the registry does not hold, refused credentials, a wrong-kind artifact, and a failure the library did not classify. A claim that is not accepted SHALL be refused on a transient registry failure too, because it has no verdict to keep. A claim whose spec was edited after its verdict (`status.observedGeneration` is behind `metadata.generation`) SHALL be refused too: its acceptance is for another spec, and the Platform reads the catalog and the version from the spec, so keeping it would keep a coordinate that was never judged.

#### Scenario: An accepted, active claim survives an unreachable registry

- **WHEN** an accepted and active claim is reconciled while the registry cannot be reached and the catalog is not in the module cache
- **THEN** `status.accepted` and `status.active` stay true, `Ready` stays `True` with reason `Accepted`, and `Active` stays `True`
- **AND** the claim reports `Reconciling=True` with reason `CatalogUnresolved`, a Warning event carries the registry error, and the claim is requeued after about 5 seconds

#### Scenario: A claim deferred after an operator restart stays accepted

- **WHEN** an accepted and active claim reports `Ready=Unknown` because an earlier reconcile found no generated platform, and its next reconcile cannot reach the registry
- **THEN** `status.accepted` and `status.active` stay true and `Active` stays `True`
- **AND** `Ready` is `Unknown` with reason `CatalogUnresolved`

#### Scenario: A 5xx answer is transient

- **WHEN** an accepted claim is reconciled and the registry answers the catalog fetch with a 5xx status
- **THEN** the claim keeps its verdict and is retried

#### Scenario: The retry backs off

- **WHEN** an accepted claim has been in this state for longer than 5 seconds
- **THEN** the next retry waits about as long as the claim has been in the state, and never longer than about 5 minutes

#### Scenario: Repeated attempts do not repeat the event

- **WHEN** an accepted claim fails a second attempt in a row with a transient registry failure
- **THEN** no second Warning event is emitted

#### Scenario: The claim is judged again when the registry answers

- **WHEN** a claim in this state is reconciled and the catalog is acquired
- **THEN** the claim is judged on the acquired catalog and no longer reports `Reconciling` with reason `CatalogUnresolved`

#### Scenario: A catalog the registry does not hold still un-accepts

- **WHEN** an accepted claim is reconciled and the registry answers that it does not hold the catalog at that version
- **THEN** the claim is refused with reason `CatalogUnresolved` and `status.accepted` is false

#### Scenario: An unclassified failure still un-accepts

- **WHEN** an accepted claim is reconciled and the acquisition fails with an error the library did not classify as a registry fetch failure
- **THEN** the claim is refused with reason `CatalogUnresolved` and `status.accepted` is false

#### Scenario: A claim that is not accepted is refused as before

- **WHEN** a claim whose `status.accepted` is false is reconciled while the registry cannot be reached
- **THEN** the claim is refused with reason `CatalogUnresolved`

#### Scenario: An edited claim is refused, not held

- **WHEN** an accepted and active claim's `spec.version` is changed and its next reconcile cannot reach the registry
- **THEN** the claim is refused with reason `CatalogUnresolved`, naming the new version, and `status.accepted` is false
