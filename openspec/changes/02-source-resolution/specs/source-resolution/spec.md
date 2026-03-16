## ADDED Requirements

### Requirement: OCIRepository lookup
The `internal/source` package MUST provide a `Resolve` function that accepts a controller-runtime client, a `SourceReference`, and a release namespace, and returns artifact metadata or a typed error.

#### Scenario: Source exists and is ready
- **WHEN** the referenced OCIRepository exists, has `Ready=True`, and has a non-nil `status.artifact`
- **THEN** `Resolve` returns an `ArtifactRef` containing the artifact URL, revision, and digest

#### Scenario: Source not found
- **WHEN** the referenced OCIRepository does not exist
- **THEN** `Resolve` returns an error wrapping `ErrSourceNotFound`

#### Scenario: Source exists but not ready
- **WHEN** the referenced OCIRepository exists but has `Ready=False` or `Ready=Unknown`
- **THEN** `Resolve` returns an error wrapping `ErrSourceNotReady`

#### Scenario: Source ready but no artifact
- **WHEN** the referenced OCIRepository has `Ready=True` but `status.artifact` is nil
- **THEN** `Resolve` returns an error wrapping `ErrSourceNotReady`

### Requirement: ArtifactRef carries full metadata
The `ArtifactRef` struct MUST expose the artifact URL, revision string, and digest string extracted from `OCIRepository.status.artifact`.

#### Scenario: All fields populated
- **WHEN** an OCIRepository has a valid `status.artifact` with URL, revision, and checksum
- **THEN** the returned `ArtifactRef` exposes all three fields

### Requirement: OCIRepository watch triggers reconciliation
The `ModuleReleaseReconciler` MUST watch `OCIRepository` objects and enqueue reconciliation for any `ModuleRelease` that references a changed source.

#### Scenario: Source artifact updates
- **WHEN** an OCIRepository's `status.artifact` changes (new revision/digest)
- **THEN** all ModuleRelease objects referencing that OCIRepository are enqueued for reconciliation

### Requirement: Typed source errors
The package MUST define sentinel errors `ErrSourceNotFound` and `ErrSourceNotReady` so callers can classify failures for condition reporting.

#### Scenario: Error classification
- **WHEN** the caller receives an error from `Resolve`
- **THEN** it can use `errors.Is` to distinguish `ErrSourceNotFound` (stalled) from `ErrSourceNotReady` (soft-blocked)
