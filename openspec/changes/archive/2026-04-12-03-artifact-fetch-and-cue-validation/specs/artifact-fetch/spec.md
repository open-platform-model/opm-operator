## ADDED Requirements

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

### Requirement: Zip extraction
The `Fetcher` MUST extract the downloaded artifact as a zip archive into a temporary directory. It MUST handle the Flux artifact format where the file path ends in `.tar.gz` but the body is a zip.

#### Scenario: Valid zip extraction
- **WHEN** the downloaded artifact is a valid zip archive containing a CUE module tree
- **THEN** the zip is extracted to a temp directory preserving the directory structure

#### Scenario: Not a zip
- **WHEN** the downloaded artifact is not a valid zip file
- **THEN** the fetcher returns an error indicating invalid artifact format

#### Scenario: Zip path traversal protection
- **WHEN** a zip entry contains path components like `../`
- **THEN** the fetcher rejects the entry and returns an error

### Requirement: CUE module layout validation
After extraction, the fetcher MUST validate that the extracted directory contains a valid CUE module layout.

#### Scenario: Valid CUE module
- **WHEN** the extracted directory contains `cue.mod/module.cue`
- **THEN** validation passes and the directory path is returned

#### Scenario: Missing cue.mod
- **WHEN** the extracted directory does not contain `cue.mod/module.cue`
- **THEN** the fetcher returns `ErrMissingCUEModule`

### Requirement: Size and count limits
The fetcher MUST enforce limits on artifact size and file count to prevent resource exhaustion.

#### Scenario: Artifact too large
- **WHEN** the artifact exceeds the configured size limit
- **THEN** the fetcher aborts the download and returns an error
