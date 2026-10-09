## ADDED Requirements

### Requirement: A token endpoint answer is judged by its status

A registry that uses token authentication has a token endpoint, and an answer from that endpoint SHALL count as an answer from the registry. When the acquisition of the claimed catalog fails because the token endpoint answered with an error status, the operator SHALL judge the claim by that status, as the library classifies it, and SHALL NOT treat the failure as an unreachable registry. This holds where the answer reaches the operator through a failed dependency load, which carries no typed status. The operator SHALL NOT classify by message text itself.

A refusal (401) SHALL refuse the claim, accepted or not, with reason `CatalogUnresolved`. A 5xx answer from the token endpoint is a transient registry failure: an accepted claim SHALL keep its verdict through it. A 429 answer (a rate limit) SHALL keep an accepted claim's verdict in the same way, with the same report and the same backoff: a rate limit passes with nothing changed and says nothing about the claim. The operator SHALL tell a 429 by the status of the library's typed fetch failure. That status does not say whether the registry or its token endpoint answered, so a 429 from either holds the claim.

#### Scenario: A refused token during a dependency load un-accepts

- **WHEN** an accepted claim is reconciled and the catalog acquisition fails while loading a dependency, because the registry's token endpoint answers 401
- **THEN** the failure is a typed fetch failure of kind unauthorized with status 401, and it is not transient
- **AND** the claim is refused with reason `CatalogUnresolved` and `status.accepted` is false

#### Scenario: A rate-limited token endpoint holds the claim

- **WHEN** the same acquisition fails because the token endpoint answers 429
- **THEN** the failure is a typed fetch failure with status 429, which the library does not class as transient
- **AND** an accepted claim keeps `status.accepted`, `status.active` and its `Ready` condition, reports `Reconciling=True` with reason `CatalogUnresolved`, and is retried on the backoff

#### Scenario: A token endpoint that is down holds the claim

- **WHEN** the same acquisition fails because the token endpoint answers 503
- **THEN** the failure is transient, and an accepted claim keeps `status.accepted`, `status.active` and its `Ready` condition
