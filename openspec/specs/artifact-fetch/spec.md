## Requirements

### Requirement: Artifact download with digest verification
The `Fetcher` implementation MUST download the artifact from the provided URL and verify the SHA-256 digest matches the expected value before extraction.

#### Scenario: Successful download
- **WHEN** the artifact URL is reachable and the downloaded content matches the expected digest
- **THEN** the artifact is saved to a temporary location for extraction

#### Scenario: Digest mismatch
- **WHEN** the downloaded artifact's SHA-256 does not match the expected digest
- **THEN** the fetcher returns an error and does not extract the artifact

#### Scenario: Download failure
- **WHEN** the artifact URL is unreachable or returns a non-200 status
- **THEN** the fetcher returns an error with context about the failure

### Requirement: Multi-format extraction
The `Fetcher` MUST support both zip and tar.gz extraction formats. The format MUST be selectable by the caller (based on source kind: OCIRepository → zip, GitRepository/Bucket → tar.gz).

#### Scenario: Valid zip extraction
- **WHEN** the caller requests zip extraction and the downloaded artifact is a valid zip archive
- **THEN** the zip is extracted to a temp directory preserving the directory structure

#### Scenario: Valid tar.gz extraction
- **WHEN** the caller requests tar.gz extraction and the downloaded artifact is a valid gzipped tar archive
- **THEN** the tar.gz is extracted to a temp directory preserving the directory structure

#### Scenario: Not a valid archive
- **WHEN** the downloaded artifact is not a valid zip or tar.gz file (depending on requested format)
- **THEN** the fetcher returns an error indicating invalid artifact format

#### Scenario: Path traversal protection (zip)
- **WHEN** a zip entry contains path components like `../`
- **THEN** the fetcher rejects the entry and returns an error

#### Scenario: Path traversal protection (tar.gz)
- **WHEN** a tar entry contains path components like `../`
- **THEN** the fetcher rejects the entry and returns an error

### Requirement: CUE module validation optional
The `Fetcher` MUST support skipping CUE module layout validation at the extraction root. When fetching for Release CRDs, the CUE module structure is at `spec.path`, not at the artifact root.

#### Scenario: Fetch without root validation
- **WHEN** the caller opts out of root-level CUE module validation
- **THEN** the fetcher extracts the artifact without checking for `cue.mod/module.cue` at the root

#### Scenario: Fetch with root validation (existing behavior)
- **WHEN** the caller requests root-level CUE module validation
- **THEN** the fetcher validates `cue.mod/module.cue` exists at the extraction root (existing behavior)

### Requirement: Size and count limits
The fetcher MUST enforce limits on artifact size and file count to prevent resource exhaustion.

#### Scenario: Artifact too large
- **WHEN** the artifact exceeds the configured size limit
- **THEN** the fetcher aborts the download and returns an error
