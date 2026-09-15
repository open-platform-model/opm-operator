## MODIFIED Requirements

### Requirement: The generated package is a function of the platform spec and the active-claim set

The operator SHALL generate the platform package from exactly two inputs: the `Platform` CR's spec, and the set of claims that are both accepted and active. Generation SHALL compute from the current state of both, never from the content of the event that woke it, so that repeated or reordered events yield the same package. A platform with no active claims SHALL generate the package it would have generated from its spec alone.

A claim whose deletion is blocked SHALL remain in the active set until the block releases. A deletion timestamp alone SHALL NOT remove a claim's catalog from the generated package: while the block holds, the claim's dependents are still rendering against that catalog, and dropping it would abandon them through the very door the block exists to close. Once the block releases the claim leaves the active set as any deleted claim does.

#### Scenario: An active claim's catalog reaches the generated package

- **WHEN** a claim becomes active
- **THEN** the regenerated package carries that provider catalog as a registry entry at the claim's version

#### Scenario: Generation ignores the waking event's content

- **WHEN** regeneration is woken by a stale or duplicated event
- **THEN** the package computed matches the one the current state implies

#### Scenario: No active claims leaves generation unchanged

- **WHEN** no claim is active
- **THEN** the generated package is a function of the `Platform` spec alone

#### Scenario: A blocked claim's catalog survives regeneration

- **WHEN** the package is regenerated while an active claim is terminating and its deletion is blocked
- **THEN** the regenerated package still carries that claim's catalog, and its dependents keep rendering

#### Scenario: A released claim leaves the active set

- **WHEN** a terminating claim's block releases
- **THEN** the next regenerated package no longer carries that claim's catalog
