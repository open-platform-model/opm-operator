## MODIFIED Requirements

### Requirement: A build-incompatible provider is refused at acceptance

Acceptance SHALL compare the catalog's committed requirements against the platform's resolved versions, per shared OPM-namespace path, and SHALL refuse the claim when the catalog requires a version greater than the platform's within the same major, or requires a major the platform does not resolve for that path (enhancement 0015 D8). Two majors of one catalog are two paths: the platform's resolution is every module its generated closure carries, disabled registry entries included, and a provider's requirement SHALL be compared against the resolved entry of the provider's own major. A requirement in a different major SHALL be refused only when no resolved path shares the provider's major, and that refusal SHALL name every major the platform resolves for the path. The verdict and its message SHALL NOT depend on the order in which the platform's resolution is read. The refusal SHALL happen here rather than at render, where the failure would name an unrelated module instance. The refusal message SHALL state that the comparison is conservative (a requirement records what the provider was tidied against, not what it uses) and that lowering the requirement is the author's fix.

Source: 0015:D8, 0026:D7:R2, 0026:D9:R7 (the shared-path comparison).

#### Scenario: A provider requiring a newer build is refused

- **WHEN** the claimed catalog requires a shared path at a version greater than the platform's resolved version, same major
- **THEN** the claim is refused, naming the path, both versions, and the one-line fix

#### Scenario: A provider on a different major is refused unconditionally

- **WHEN** the claimed catalog requires a shared path in a different major than every major the platform resolves for that path
- **THEN** the claim is refused without comparing versions, naming every major the platform resolves for that path

#### Scenario: A provider requiring an older build is accepted by this check

- **WHEN** every shared requirement is at or below the platform's resolved version within the same major
- **THEN** this check does not refuse the claim

#### Scenario: A provider is compared against the resolved entry of its own major

- **WHEN** the platform resolves `opmodel.dev/catalogs/opm@v4` at `v4.2.0` and `opmodel.dev/catalogs/opm@v5` at `v5.0.0` (for example an enabled v4 entry beside a disabled v5 entry), and the claimed catalog requires `opmodel.dev/catalogs/opm@v4` at `v4.1.0`
- **THEN** this check does not refuse the claim, and a catalog requiring `opmodel.dev/catalogs/opm@v5` at `v5.1.0` is refused as requiring a newer build than `v5.0.0`, never as a major mismatch

#### Scenario: The verdict does not depend on iteration order

- **WHEN** the same claim is judged repeatedly against a platform resolving several majors of a path the claimed catalog requires
- **THEN** every judgement reaches the same verdict with a byte-identical message
