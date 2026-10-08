## ADDED Requirements

### Requirement: The staged apply order never contradicts the library's kind weights
The order in which the staged apply writes two objects of different kinds MUST NOT be the opposite of the order the library's kind weights give them. When Flux's staged apply, at the version the operator pins, applies an object of kind A before an object of kind B, the library's weight of A, at the version the operator pins, MUST NOT be higher than its weight of B. Flux's order MAY refine the library's order: it MAY put two kinds that the library weighs equally in an order of its own. Source: 0012:D5:R1.

The operator's test suite MUST fail when the two pinned versions break this rule. The check MUST read Flux's order from the pinned Flux module itself (its stage rules and its sort), not from a copy of Flux's kind list, so that a bump of the Flux pin that moves a kind fails the suite without an edit to the check. The check MUST cover every kind Flux's order names, every kind the library's weight table names, the same kind names in an API group of another owner, a custom kind and a custom kind whose name ends in `Class`.

These cases are not a failure by themselves, and the check MUST report them instead of hiding them: two kinds of equal library weight that Flux orders; a kind that only one of the two orders names, which is compared by the weight the library's fallback rules give it and by the place Flux's fallback rules give it; two objects of the same group and kind, which neither order separates by kind.

A failure MUST name every pair of kinds that the two orders put the opposite way round, with the library weight of each. It MUST tell the maintainer that the library's weight table is what changes to follow Flux, that the pin bump waits for that library release, and that the check and its list of kinds are not edited to make it pass.

#### Scenario: The pinned versions agree
- **WHEN** the test suite runs with the Flux version and the library version of `go.mod`
- **THEN** the check finds no pair of kinds in opposite order and passes

#### Scenario: A Flux bump moves a kind
- **WHEN** the Flux pin moves to a version whose reconcile order puts a Deployment before a Service, and the library still weighs a Service below a Deployment
- **THEN** the check fails without any edit to it
- **AND** the failure names the pair Deployment and Service with both weights

#### Scenario: A Flux bump adds a kind to its order
- **WHEN** the Flux pin moves to a version whose reconcile order newly names a kind, at a place that is the opposite of the weight the library's fallback gives that kind
- **THEN** the check fails and names the new kind

#### Scenario: A library bump moves a weight
- **WHEN** the library pin moves to a version that weighs a Deployment below a Service, and Flux still applies a Service before a Deployment
- **THEN** the check fails and names the pair

#### Scenario: Flux refines a tie
- **WHEN** the library weighs a ConfigMap and a Secret equally and Flux applies the ConfigMap first
- **THEN** the check does not fail for that pair
- **AND** the check reports how many such pairs it found

#### Scenario: A kind that only one order names
- **WHEN** a kind is named by the library's weight table and not by Flux's reconcile order, or the other way round, and no pair with it is in opposite order
- **THEN** the check does not fail
- **AND** a kind of Flux's order for which the check has no API group of its own is reported by name

#### Scenario: The failure says what to do
- **WHEN** the check fails
- **THEN** the message says that the library's weight table and the Flux list of the library's own guard change first, that the pin bump waits for that release, and that this check is not edited to pass

#### Scenario: A real staged apply follows the weights
- **WHEN** `Apply` applies a new set that holds a CustomResourceDefinition, a Namespace, a ClusterRole, a class kind, workloads, configuration, a webhook configuration and a custom resource to a real API server
- **THEN** every cluster definition is written before every class kind, and every class kind before every other object
- **AND** the library weight never decreases along the order of the writes
