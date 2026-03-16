## Package: `internal/apply`

### Files

| File | Purpose |
|------|---------|
| `prune.go` | `Prune` function and `PruneResult` type (replaces empty `Prune` stub). Design decisions 1-3. |
| `prune_test.go` | envtest-based tests for prune, safety exclusions, already-deleted, empty stale set |

### Imports

```go
import (
    "context"
    "errors"
    "fmt"

    apierrors "k8s.io/apimachinery/pkg/api/errors"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client"
    logf "sigs.k8s.io/controller-runtime/pkg/log"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)
```

### Types — `prune.go`

```go
// PruneResult carries counts of prune outcomes.
type PruneResult struct {
    // Deleted is the number of stale resources successfully deleted.
    Deleted int

    // Skipped is the number of stale resources skipped due to safety exclusions.
    Skipped int
}
```

### Functions — `prune.go`

```go
// Prune deletes stale resources from the cluster.
// Uses direct client.Delete per resource rather than Flux's DeleteAll to allow
// per-resource error control and safety exclusion logic (design decision 1).
//
// Safety exclusions (design decision 3: hard-coded, not configurable):
//   - Namespace: never auto-deleted (cascades to all resources inside)
//   - CustomResourceDefinition: never auto-deleted (deletes all instances globally)
// Skipped resources are logged as warnings and counted in PruneResult.Skipped.
//
// If a stale resource is already gone (NotFound), it is treated as success.
// Individual delete failures are collected and returned as a joined error;
// remaining deletes continue (design decision 2: continue-on-error / fail-slow).
//
// The caller is responsible for:
//   - Computing the stale set via internal/inventory.ComputeStaleSet (change 1)
//   - Checking spec.prune before calling this function
//   - Ensuring apply succeeded before calling prune
func Prune(
    ctx context.Context,
    c client.Client,
    stale []releasesv1alpha1.InventoryEntry,
) (*PruneResult, error)

// isSafeToDelete returns false for Namespace and CustomResourceDefinition kinds.
func isSafeToDelete(entry releasesv1alpha1.InventoryEntry) bool
```

### Safety Exclusion Kinds

```go
// Excluded from pruning per docs/design/ssa-ownership-and-drift-policy.md:
//   - "Namespace"                  — cascading deletion destroys all contained resources
//   - "CustomResourceDefinition"   — deletion removes all CRs globally across the cluster
```
