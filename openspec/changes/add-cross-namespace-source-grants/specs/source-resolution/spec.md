## MODIFIED Requirements

### Requirement: OCIRepository lookup
The `internal/source` package MUST provide a `Resolve` function that accepts a controller-runtime client, a `SourceReference`, a release namespace, and a `CrossNamespacePolicy`, and returns artifact metadata or a typed error. The function MUST support OCIRepository, GitRepository, and Bucket source kinds. When `SourceReference.Namespace` is empty or equal to the release namespace, `Resolve` MUST look up the source in the release namespace. When `SourceReference.Namespace` names a different namespace, `Resolve` MUST consult the `CrossNamespacePolicy` and MUST NOT read the foreign source unless the policy permits the reference.

#### Scenario: Source exists and is ready
- **WHEN** the referenced source (OCIRepository, GitRepository, or Bucket) exists in the release namespace, has `Ready=True`, and has a non-nil `status.artifact`
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

#### Scenario: Cross-namespace reference denied by default
- **WHEN** `SourceReference.Namespace` names a namespace other than the release namespace **AND** the supplied `CrossNamespacePolicy` does not permit the reference
- **THEN** `Resolve` returns an error wrapping `ErrCrossNamespaceForbidden` and MUST NOT read the foreign source object

#### Scenario: Cross-namespace reference permitted by policy
- **WHEN** `SourceReference.Namespace` names a namespace other than the release namespace **AND** the supplied `CrossNamespacePolicy` permits the reference
- **THEN** `Resolve` looks up the source in the referenced namespace and proceeds as for a same-namespace lookup

## ADDED Requirements

### Requirement: Cross-namespace resolution surfaces as a stalled release
The `ReleaseReconciler` MUST treat a cross-namespace denial from `Resolve` as a stalled condition rather than a transient one, because it cannot self-heal without an operator action (enabling the flag or authoring a grant).

#### Scenario: Forbidden cross-namespace reference stalls the release
- **WHEN** `resolveReleaseSource` receives an error wrapping `ErrCrossNamespaceForbidden`
- **THEN** the Release is marked `Stalled` with a reason identifying the forbidden cross-namespace source, a Warning event is emitted, and the release is requeued on the stalled recheck interval (not the transient interval)

### Requirement: Typed cross-namespace error
The `internal/source` package MUST define a sentinel error `ErrCrossNamespaceForbidden` so callers can classify a denied cross-namespace reference distinctly from not-found / not-ready / unsupported-kind failures.

#### Scenario: Error classification
- **WHEN** the caller receives a denial from `Resolve` for a cross-namespace reference
- **THEN** it can use `errors.Is(err, ErrCrossNamespaceForbidden)` to distinguish it from `ErrSourceNotFound`, `ErrSourceNotReady`, and `ErrUnsupportedSourceKind`
