## Package: `internal/apply`

### Files

| File | Purpose |
|------|---------|
| `manager.go` | `NewResourceManager` constructor, replaces existing `ResourceManager` type alias with richer setup (design decision 1: use Flux ApplyAllStaged) |
| `apply.go` | `Apply` function with staged resource ordering (design decision 2: resourceorder for staging) |
| `apply_test.go` | envtest-based tests for apply, ordering, idempotency, force-conflicts |

### Imports

```go
import (
    "context"
    "fmt"
    "sort"

    fluxssa "github.com/fluxcd/pkg/ssa"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/open-platform-model/poc-controller/pkg/resourceorder"
)
```

### Constants

```go
const (
    // FieldManager is the SSA field manager name used by the controller.
    // Distinguishes from "opm-cli", "kubectl", "helm", etc.
    // From docs/design/ssa-ownership-and-drift-policy.md.
    FieldManager = "opm-controller"

    // StageOneThreshold is the weight threshold for stage 1 resources.
    // Resources with resourceorder.GetWeight(gvk) < StageOneThreshold go to stage 1
    // (CRDs, Namespaces, RBAC, ServiceAccounts, Secrets, ConfigMaps, etc.).
    // Everything else goes to stage 2.
    StageOneThreshold = 100
)
```

### Types — `apply.go`

```go
// ApplyResult carries counts of apply outcomes.
// Simple counter struct with no per-resource detail for v1alpha1 (design decision 3).
type ApplyResult struct {
    // Created is the number of resources created (did not exist before).
    Created int

    // Updated is the number of resources updated (existed, fields changed).
    Updated int

    // Unchanged is the number of resources unchanged (existed, no field diff).
    Unchanged int
}
```

### Functions — `manager.go`

```go
// NewResourceManager constructs a Flux SSA ResourceManager with the opm-controller
// field manager (design decision 1). The owner string is used for SSA ownership labels.
func NewResourceManager(c client.Client, owner string) *fluxssa.ResourceManager
```

### Functions — `apply.go`

```go
// Apply applies the given resources to the cluster using Server-Side Apply.
// Resources are sorted into two stages using resourceorder.GetWeight (design decision 2):
//   - Stage 1: CRDs, Namespaces, RBAC, ServiceAccounts, etc. (weight < StageOneThreshold)
//   - Stage 2: Deployments, Services, and all other resources
//
// Uses fluxssa.ResourceManager.ApplyAllStaged for the two-stage apply.
// When force is true, SSA force-conflicts is enabled (spec.rollout.forceConflicts).
//
// Returns an ApplyResult with counts, or an error on any apply failure.
func Apply(
    ctx context.Context,
    rm *fluxssa.ResourceManager,
    resources []*unstructured.Unstructured,
    force bool,
) (*ApplyResult, error)

// stageResources sorts resources into stage 1 and stage 2 by apply weight.
// Uses resourceorder.GetWeight(gvk) from the locally copied pkg/resourceorder.
func stageResources(
    resources []*unstructured.Unstructured,
) (stage1, stage2 []*unstructured.Unstructured)
```

### Package Reference Types

```go
// resourceorder.GetWeight(gvk schema.GroupVersionKind) int
//   Returns apply-ordering weight. Lower = applied first.
//   WeightCRD = -100, WeightNamespace = 0, WeightDeployment = 100, WeightDefault = 1000.
//
// fluxssa.ResourceManager.ApplyAllStaged(ctx, objects, opts) (*fluxssa.ChangeSet, error)
//   Applies objects in staged order. ChangeSet tracks action per resource.
//
// fluxssa.ChangeSet.Entries []fluxssa.ChangeSetEntry
//   Each entry has .Action: "created", "configured" (updated), "unchanged".
```
