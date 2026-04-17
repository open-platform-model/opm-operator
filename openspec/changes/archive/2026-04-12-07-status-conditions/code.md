## Packages: `internal/status` + `api/v1alpha1`

### Files

| File | Purpose |
|------|---------|
| `internal/status/conditions.go` | Condition type constants, reason constants, helper functions (design decisions 1-3) |
| `internal/status/conditions_test.go` | Condition transition tests, interface compliance test |
| `api/v1alpha1/conditions.go` | `GetConditions()` and `SetConditions()` methods on `ModuleRelease` (design decision 2: Flux interface compliance) |

### Imports

```go
// internal/status/conditions.go
import (
    "fmt"

    "github.com/fluxcd/pkg/runtime/conditions"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

// api/v1alpha1/conditions.go
import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
```

### Constants — `internal/status/conditions.go`

```go
// Condition types matching docs/design/module-release-reconcile-loop.md.
const (
    ReadyCondition       = "Ready"
    ReconcilingCondition = "Reconciling"
    StalledCondition     = "Stalled"
    SourceReadyCondition = "SourceReady"
)

// Condition reasons matching docs/design/module-release-reconcile-loop.md.
// Follow Kubernetes convention: PascalCase string constants (design decision 3).
const (
    SuspendedReason              = "Suspended"
    SourceNotReadyReason         = "SourceNotReady"
    SourceUnavailableReason      = "SourceUnavailable"
    ArtifactFetchFailedReason    = "ArtifactFetchFailed"
    ArtifactInvalidReason        = "ArtifactInvalid"
    RenderFailedReason           = "RenderFailed"
    ApplyFailedReason            = "ApplyFailed"
    PruneFailedReason            = "PruneFailed"
    ReconciliationSucceededReason = "ReconciliationSucceeded"
)
```

### API Methods — `api/v1alpha1/conditions.go`

```go
// GetConditions returns the status conditions of the ModuleRelease.
// Implements fluxcd/pkg/runtime/conditions.Getter (design decision 2).
func (in *ModuleRelease) GetConditions() []metav1.Condition {
    return in.Status.Conditions
}

// SetConditions sets the status conditions on the ModuleRelease.
// Implements fluxcd/pkg/runtime/conditions.Setter (design decision 2).
func (in *ModuleRelease) SetConditions(conditions []metav1.Condition) {
    in.Status.Conditions = conditions
}
```

### Helper Functions — `internal/status/conditions.go`

```go
// MarkReconciling sets Reconciling=True and Ready=Unknown on the object.
// Uses fluxcd/pkg/runtime/conditions helpers (design decision 1: thin Flux wrappers).
func MarkReconciling(obj conditions.Setter, reason, message string)

// MarkStalled sets Stalled=True and Ready=False on the object.
func MarkStalled(obj conditions.Setter, reason, message string)

// MarkReady sets Ready=True and removes Reconciling and Stalled conditions.
func MarkReady(obj conditions.Setter, message string)

// MarkNotReady sets Ready=False with the given reason and message.
func MarkNotReady(obj conditions.Setter, reason, message string)

// MarkSourceReady sets SourceReady=True with the artifact revision as message.
func MarkSourceReady(obj conditions.Setter, revision string)

// MarkSourceNotReady sets SourceReady=False with the given reason and message.
func MarkSourceNotReady(obj conditions.Setter, reason, message string)
```

### Flux Interface Compliance

```go
// The following must be verified at compile time or in tests:
var _ conditions.Getter = (*releasesv1alpha1.ModuleRelease)(nil)
var _ conditions.Setter = (*releasesv1alpha1.ModuleRelease)(nil)
```

### Note on `SerialPatcher`

`fluxcd/pkg/runtime/patch.SerialPatcher` is referenced in the design but will be
integrated in change 11 (reconcile loop assembly) where the patch lifecycle is managed.
This change only provides the condition manipulation helpers it calls.
