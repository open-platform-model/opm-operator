## Package: `internal/status`

### Files

| File | Purpose |
|------|---------|
| `digests.go` | `DigestSet` type, `ConfigDigest`, `RenderDigest`, `SourceDigest`, `IsNoOp` (replaces empty `Digest` stub). Design decisions 1-3. |
| `digests_test.go` | Determinism, content sensitivity, no-op detection tests |

### Imports

```go
import (
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "sort"

    "github.com/open-platform-model/poc-controller/pkg/core"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)
```

### Types — `digests.go`

```go
// DigestSet holds the four reconcile digests tracked in ModuleRelease.status.
// Uses named fields rather than a map for type safety (design decision 3).
//
// Maps to status fields:
//   Source    → lastAttemptedSourceDigest / lastAppliedSourceDigest
//   Config    → lastAttemptedConfigDigest / lastAppliedConfigDigest
//   Render    → lastAttemptedRenderDigest / lastAppliedRenderDigest
//   Inventory → status.inventory.digest
type DigestSet struct {
    // Source is the artifact content digest from Flux OCIRepository.status.artifact.
    Source string

    // Config is the SHA-256 of normalized user values.
    Config string

    // Render is the SHA-256 of the sorted, serialized rendered resource set.
    Render string

    // Inventory is the SHA-256 of the owned resource inventory
    // (computed via internal/inventory.ComputeDigest from change 1).
    Inventory string
}
```

### Functions — `digests.go`

```go
// SourceDigest returns the source digest as-is from the Flux artifact.
// This is a passthrough — Flux already computed the digest.
func SourceDigest(artifactDigest string) string

// ConfigDigest computes a deterministic SHA-256 digest of the release values.
// Serializes RawValues to canonical JSON (sorted keys), then hashes.
// Returns empty string if values is nil (design decision 1: nil = no config).
// Format: "sha256:<hex>"
func ConfigDigest(values *releasesv1alpha1.RawValues) string

// RenderDigest computes a deterministic SHA-256 digest of the rendered resource set.
// Sorts resources by GVK + namespace + name (same order as inventory.ComputeDigest
// for consistency, design decision 2), serializes each via core.Resource.MarshalJSON(),
// and hashes the concatenation.
// Format: "sha256:<hex>"
func RenderDigest(resources []*core.Resource) (string, error)

// IsNoOp returns true if all four digests in current match lastApplied.
// Returns false if any lastApplied field is empty (handles first reconcile,
// per spec scenario "Empty last applied").
func IsNoOp(current, lastApplied DigestSet) bool
```

### Integration with Status Fields

```go
// The reconcile loop (change 11) uses DigestSet to populate status:
//
//   status.lastAttemptedSourceDigest = digests.Source
//   status.lastAttemptedConfigDigest = digests.Config
//   status.lastAttemptedRenderDigest = digests.Render
//
// On full success:
//   status.lastAppliedSourceDigest  = digests.Source
//   status.lastAppliedConfigDigest  = digests.Config
//   status.lastAppliedRenderDigest  = digests.Render
//   status.inventory.digest         = digests.Inventory
```
