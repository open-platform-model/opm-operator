## Package: `internal/inventory`

### Files

| File | Purpose |
|------|---------|
| `inventory.go` | Keep existing `type Current = releasesv1alpha1.Inventory` alias (design decision 3) |
| `entry.go` | `NewEntryFromResource`, `IdentityEqual`, `K8sIdentityEqual`, label constant (copied from CLI) |
| `stale.go` | `ComputeStaleSet` (copied from CLI, replaces empty `StaleSet` stub) |
| `digest.go` | `ComputeDigest` (copied from CLI, replaces empty `Digest` stub) |
| `entry_test.go` | Identity comparison and entry construction tests |
| `stale_test.go` | Stale set computation tests |
| `digest_test.go` | Digest determinism and content sensitivity tests |

### Imports

```go
import (
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "sort"

    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)
```

### Existing Types (preserved)

```go
// inventory.go — kept as-is per design decision 3
type Current = releasesv1alpha1.Inventory
```

### CRD Type (read-only reference)

```go
// api/v1alpha1.InventoryEntry — the authoritative type, used directly
type InventoryEntry struct {
    Group     string `json:"group,omitempty"`
    Kind      string `json:"kind"`
    Namespace string `json:"namespace,omitempty"`
    Name      string `json:"name"`
    Version   string `json:"v,omitempty"`
    Component string `json:"component,omitempty"`
}
```

### Constants — `entry.go`

```go
// LabelComponentName is the label key for the source component name.
const LabelComponentName = "component.opmodel.dev/name"
```

### Functions — `entry.go`

```go
// NewEntryFromResource creates an inventory entry from an unstructured Kubernetes resource.
// Extracts GVK, namespace, name, and the component label.
func NewEntryFromResource(r *unstructured.Unstructured) releasesv1alpha1.InventoryEntry

// IdentityEqual returns true if two entries identify the same owned resource.
// Compares Group, Kind, Namespace, Name, and Component. Version is excluded
// to prevent false orphans during Kubernetes API version migrations.
func IdentityEqual(a, b releasesv1alpha1.InventoryEntry) bool

// K8sIdentityEqual returns true if two entries identify the same Kubernetes resource.
// Compares Group, Kind, Namespace, and Name only (excludes Version and Component).
func K8sIdentityEqual(a, b releasesv1alpha1.InventoryEntry) bool
```

### Functions — `stale.go`

```go
// ComputeStaleSet returns entries present in previous but absent from current.
// Uses IdentityEqual for comparison, meaning Version changes do not produce
// stale entries.
func ComputeStaleSet(
    previous, current []releasesv1alpha1.InventoryEntry,
) []releasesv1alpha1.InventoryEntry
```

### Functions — `digest.go`

```go
// ComputeDigest returns a deterministic SHA-256 digest of the inventory entries.
// Entries are sorted by Group, Kind, Namespace, Name, Component, Version before
// hashing. Returns a string in the format "sha256:<hex>".
func ComputeDigest(entries []releasesv1alpha1.InventoryEntry) string
```
