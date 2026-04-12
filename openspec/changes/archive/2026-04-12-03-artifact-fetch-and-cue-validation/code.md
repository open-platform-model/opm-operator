## Package: `internal/source`

### Files

| File | Purpose |
|------|---------|
| `fetch.go` | `ArtifactFetcher` struct implementing `Fetcher` interface (design decision 1: direct HTTP, not Flux ArchiveFetcher) |
| `extract.go` | `extractZip` helper using Go stdlib `archive/zip` (design decision 2: stdlib zip) |
| `validate.go` | Expanded `ValidateCUEModule(dir)` function (validates `cue.mod/module.cue` exists) |
| `fetch_test.go` | HTTP fetch with digest verification tests |
| `extract_test.go` | Zip extraction tests (valid, invalid, path traversal, empty) |
| `validate_test.go` | CUE module layout validation tests |
| `testdata/` | Test fixture zip archives |

### Imports

```go
import (
    "archive/zip"
    "context"
    "crypto/sha256"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
)
```

### Constants

```go
const (
    // MaxArtifactSize is the maximum allowed artifact download size (64 MB).
    // Protects against resource exhaustion from oversized artifacts.
    MaxArtifactSize = 64 << 20 // 64 MiB

    // MaxZipFiles is the maximum number of files allowed in a zip archive.
    MaxZipFiles = 10000
)
```

### Existing Interface (from `fetch.go` — preserved)

```go
// Fetcher fetches a resolved source artifact into a local directory.
type Fetcher interface {
    Fetch(ctx context.Context, artifactURL, artifactDigest, dir string) error
}
```

### Types — `fetch.go`

```go
// ArtifactFetcher downloads OCI artifacts via HTTP and extracts them as zip files.
// Implements the Fetcher interface.
//
// Uses direct HTTP fetch with SHA-256 digest verification rather than Flux's
// ArchiveFetcher, because Flux assumes tar.gz format while CUE OCI artifacts
// use zip (design decision 1).
//
// The caller is responsible for temp directory creation and cleanup
// (design decision 3: fetcher does not manage lifecycle).
type ArtifactFetcher struct {
    // HTTPClient is the HTTP client used for downloads. If nil, http.DefaultClient is used.
    HTTPClient *http.Client

    // MaxSize is the maximum artifact size in bytes. Defaults to MaxArtifactSize.
    MaxSize int64
}
```

### Functions — `fetch.go`

```go
// Fetch downloads the artifact from url, verifies its SHA-256 digest matches
// artifactDigest, extracts the zip contents to dir, and validates the CUE
// module layout.
//
// The artifact body is expected to be a zip file despite the URL path possibly
// ending in .tar.gz (Flux artifact format quirk confirmed in experiment 001).
//
// Returns ErrMissingCUEModule if the extracted directory lacks cue.mod/module.cue.
func (f *ArtifactFetcher) Fetch(
    ctx context.Context,
    artifactURL, artifactDigest, dir string,
) error
```

### Functions — `extract.go`

```go
// extractZip extracts the zip archive at zipPath into destDir.
// Rejects entries with path traversal components ("..") to prevent
// zip slip attacks. Enforces MaxZipFiles limit.
func extractZip(zipPath, destDir string) error
```

### Functions — `validate.go`

```go
// ValidateCUEModule checks that dir contains a valid CUE module layout.
// Currently validates that cue.mod/module.cue exists and is non-empty.
// Returns ErrMissingCUEModule on failure.
func ValidateCUEModule(dir string) error
```
