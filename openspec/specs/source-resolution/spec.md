## Requirements

### Requirement: OCIRepository lookup
The `internal/source` package MUST provide a `Resolve` function that accepts a controller-runtime client, a `SourceReference`, and a release namespace, and returns artifact metadata or a typed error. The function MUST support OCIRepository, GitRepository, and Bucket source kinds.

#### Scenario: Source exists and is ready
- **WHEN** the referenced source (OCIRepository, GitRepository, or Bucket) exists, has `Ready=True`, and has a non-nil `status.artifact`
- **THEN** `Resolve` returns an `ArtifactRef` containing the artifact URL, revision, and digest

#### Scenario: Source not found
- **WHEN** the referenced source does not exist
- **THEN** `Resolve` returns an error wrapping `ErrSourceNotFound`

#### Scenario: Source exists but not ready
- **WHEN** the referenced source exists but has `Ready=False` or `Ready=Unknown`
- **THEN** `Resolve` returns an error wrapping `ErrSourceNotReady`

#### Scenario: Source ready but no artifact
- **WHEN** the referenced source has `Ready=True` but `status.artifact` is nil
- **THEN** `Resolve` returns an error wrapping `ErrSourceNotReady`

#### Scenario: Unsupported source kind
- **WHEN** the `SourceReference.Kind` is not one of `OCIRepository`, `GitRepository`, or `Bucket`
- **THEN** `Resolve` returns an error wrapping `ErrUnsupportedSourceKind`

### Requirement: ArtifactRef carries full metadata
The `ArtifactRef` struct MUST expose the artifact URL, revision string, and digest string extracted from the source object's `status.artifact`.

#### Scenario: All fields populated
- **WHEN** a source has a valid `status.artifact` with URL, revision, and checksum
- **THEN** the returned `ArtifactRef` exposes all three fields

### Requirement: Source watch triggers reconciliation
The `ReleaseReconciler` MUST watch OCIRepository, GitRepository, and Bucket objects and enqueue reconciliation for any Release that references a changed source.

#### Scenario: Source artifact updates
- **WHEN** a source object's `status.artifact` changes (new revision/digest)
- **THEN** all Release objects referencing that source are enqueued for reconciliation

### Requirement: Typed source errors
The package MUST define sentinel errors `ErrSourceNotFound`, `ErrSourceNotReady`, and `ErrUnsupportedSourceKind` so callers can classify failures for condition reporting.

#### Scenario: Error classification
- **WHEN** the caller receives an error from `Resolve`
- **THEN** it can use `errors.Is` to distinguish `ErrSourceNotFound`, `ErrSourceNotReady`, and `ErrUnsupportedSourceKind`
