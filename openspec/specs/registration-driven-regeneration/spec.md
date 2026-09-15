# registration-driven-regeneration Specification

## Purpose

Define what the operator-generated platform package is a function of once claims can be active, what wakes it, how it is identified, and how long a generated package remains readable. Enhancement 0015 D13 states the function and the triggers; D17 states the unit the operator holds.

Judging a claim is not part of this capability. Acceptance and activation decide `status.accepted` and `status.active`; this capability consumes the result.

## Requirements

### Requirement: The generated package is a function of the platform spec and the active-claim set

The operator SHALL generate the platform package from exactly two inputs: the `Platform` CR's spec, and the set of claims that are both accepted and active. Generation SHALL compute from the current state of both, never from the content of the event that woke it, so that repeated or reordered events yield the same package. A platform with no active claims SHALL generate the package it would have generated from its spec alone.

#### Scenario: An active claim's catalog reaches the generated package

- **WHEN** a claim becomes active
- **THEN** the regenerated package carries that provider catalog as a registry entry at the claim's version

#### Scenario: Generation ignores the waking event's content

- **WHEN** regeneration is woken by a stale or duplicated event
- **THEN** the package computed matches the one the current state implies

#### Scenario: No active claims leaves generation unchanged

- **WHEN** no claim is active
- **THEN** the generated package is a function of the `Platform` spec alone

### Requirement: Regeneration is edge-triggered by every input

The operator SHALL wake regeneration when the `Platform` CR changes and when any `TransformerRegistration` changes, so a claim activating does not wait for an unrelated reconcile. Bursts SHALL coalesce: several claims becoming active together SHALL converge in one or a few regenerations rather than one per claim.

#### Scenario: Activating a claim wakes regeneration

- **WHEN** a claim transitions to active
- **THEN** the platform package is regenerated without the `Platform` CR being edited

#### Scenario: A burst of activations coalesces

- **WHEN** several claims become active in quick succession
- **THEN** the number of regenerations is bounded well below the number of claims, and the final package reflects all of them

### Requirement: A generated package is identified by its inputs

Each generated package SHALL carry an identity derived from the `Platform` CR's generation and the sorted set of active claims' catalog coordinates. Two packages generated from the same inputs SHALL carry the same identity, and any change to either input SHALL yield a different one. The identity SHALL be readable from `Platform` status, and every render SHALL report the identity of the package it consumed.

#### Scenario: A claim change yields a new identity without a spec change

- **WHEN** a claim activates while the `Platform` CR's generation is unchanged
- **THEN** the regenerated package carries a different identity than the previous one

#### Scenario: A render reports what it consumed

- **WHEN** a render completes
- **THEN** the identity of the package it built against is reported, so the render is attributable to an exact registry state

### Requirement: The effective registry is readable from the Platform

`Platform` status SHALL carry the resolved union of the registry the platform is running: the subscriptions its spec authored and the catalogs its active claims contributed. An entry SHALL indicate which of the two it came from, so an operator can tell an authored subscription from one a provider registered. An entry SHALL also carry the version the package pins it at and whether its transformers register, because a disabled subscription belongs in the union — it is still pinned and imported — but contributes no transformer, and a union read without that distinction overstates what the platform runs.

#### Scenario: The union shows both sources

- **WHEN** a platform has an authored subscription and an active claim
- **THEN** the status union lists both, distinguishing which is which

#### Scenario: A disabled subscription is listed as not registering

- **WHEN** a platform authors a subscription with transformer registration disabled
- **THEN** the status union lists its catalog and marks it as contributing no transformer

#### Scenario: The union follows the active set

- **WHEN** a claim stops being active
- **THEN** the union no longer lists its catalog

### Requirement: A package stays readable while a render holds it

A generated package SHALL remain readable for as long as any render is consuming it, even after a newer package has been generated. Regeneration SHALL NOT invalidate a render already in progress, and a package SHALL be reclaimed only once nothing holds it.

#### Scenario: Regeneration during a render does not disturb it

- **WHEN** a package is regenerated while a render is reading the previous one
- **THEN** that render completes against the package it started with

#### Scenario: A superseded package is reclaimed once released

- **WHEN** the last render holding a superseded package finishes
- **THEN** that package is reclaimed
