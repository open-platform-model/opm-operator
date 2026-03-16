## Package: `internal/status`

### Files

| File | Purpose |
|------|---------|
| `history.go` | Entry construction helpers, bounded append, sequence management (replaces empty `History` stub). Design decisions 1-3. |
| `history_test.go` | Append, trim, ordering, sequence, timestamp tests |

### Imports

```go
import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)
```

### Constants

```go
const (
    // MaxHistoryEntries is the maximum number of history entries retained.
    // Matches docs/design/module-release-reconcile-loop.md retention policy
    // (design decision 1).
    MaxHistoryEntries = 10
)
```

### CRD Type Reference (read-only — from `api/v1alpha1/common_types.go`)

```go
// v1alpha1.HistoryEntry — the type each entry is stored as in status.history.
type HistoryEntry struct {
    Sequence        int64        `json:"sequence,omitempty"`
    Action          string       `json:"action,omitempty"`
    Phase           string       `json:"phase,omitempty"`
    StartedAt       *metav1.Time `json:"startedAt,omitempty"`
    FinishedAt      *metav1.Time `json:"finishedAt,omitempty"`
    SourceDigest    string       `json:"sourceDigest,omitempty"`
    ConfigDigest    string       `json:"configDigest,omitempty"`
    RenderDigest    string       `json:"renderDigest,omitempty"`
    InventoryDigest string       `json:"inventoryDigest,omitempty"`
    InventoryCount  int64        `json:"inventoryCount,omitempty"`
    Message         string       `json:"message,omitempty"`
}
```

### Functions — `history.go`

```go
// NewSuccessEntry creates a HistoryEntry for a successful reconcile action.
// Populates timestamps via metav1.Now() automatically (design decision 2: typed helpers).
// Digest fields are populated from the DigestSet (change 6).
func NewSuccessEntry(
    action string,
    phase string,
    digests DigestSet,
    inventoryCount int64,
) releasesv1alpha1.HistoryEntry

// NewFailureEntry creates a HistoryEntry for a failed reconcile attempt.
// Populates timestamps via metav1.Now(). Digests may be partially filled
// depending on which phase failed.
func NewFailureEntry(
    action string,
    message string,
    digests DigestSet,
) releasesv1alpha1.HistoryEntry

// RecordHistory prepends entry to status.History and trims to MaxHistoryEntries.
// The entry's Sequence is set to nextSequence(status.History) before prepending.
// Newest entry is at index 0 after prepend.
//
// Does not record periodic no-ops (caller responsibility — design doc explicitly
// excludes recording every periodic no-op).
func RecordHistory(
    status *releasesv1alpha1.ModuleReleaseStatus,
    entry releasesv1alpha1.HistoryEntry,
)

// nextSequence returns max(existing sequences) + 1 for monotonic ordering
// (design decision 3). Returns 1 if history is empty.
func nextSequence(history []releasesv1alpha1.HistoryEntry) int64
```
