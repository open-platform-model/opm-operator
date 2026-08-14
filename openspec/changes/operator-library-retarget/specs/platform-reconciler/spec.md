# platform-reconciler — Delta

## MODIFIED Requirements

### Requirement: Subscription mapping and materialize failures

The Platform reconciler SHALL map each subscription's `version` into platform synthesis verbatim (no filter mapping exists). A subscription with an empty version SHALL surface as `MaterializeFailed` with a message naming the subscription path (the library refuses it before any registry I/O). A version absent from the registry SHALL surface as `MaterializeFailed` carrying the published versions within the key's major. A pulled catalog whose declared identity disagrees with the subscription coordinate SHALL surface as `MaterializeFailed` carrying both the declared and fetched values (typed identity error wrapped in the materialize error). All three retain the stalled recheck interval.

#### Scenario: Missing version surfaces as MaterializeFailed

- **WHEN** the stored Platform carries a subscription without a version
- **THEN** the Platform's Ready condition is False with reason MaterializeFailed and a message naming the subscription path

#### Scenario: Named build not published

- **WHEN** a subscription names a version the registry does not have
- **THEN** MaterializeFailed carries the missing version and the published list scoped to the key's major

#### Scenario: Identity mismatch surfaces with both values

- **WHEN** a pulled catalog's declared modulePath or version disagrees with the subscription coordinate
- **THEN** MaterializeFailed carries both the declared and the fetched value
