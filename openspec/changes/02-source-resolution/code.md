## Package: `internal/source`

### Files

| File | Purpose |
|------|---------|
| `artifact.go` | Expanded `ArtifactRef` with URL, Revision, Digest fields (design decision 1: structured result, not raw Flux types) |
| `resolve.go` | `Resolve` function — OCIRepository lookup, readiness validation, artifact extraction |
| `validate.go` | Sentinel errors `ErrSourceNotFound`, `ErrSourceNotReady` (added alongside existing `ErrMissingCUEModule`) |
| `resolve_test.go` | Unit tests with fake controller-runtime client |

### Also Modifies

| File | Purpose |
|------|---------|
| `internal/controller/modulerelease_controller.go` | Add OCIRepository watch via `handler.EnqueueRequestsFromMapFunc` (design decision 3) |

### Imports

```go
import (
    "context"
    "errors"
    "fmt"

    sourcev1 "github.com/fluxcd/source-controller/api/v1"
    fluxmeta "github.com/fluxcd/pkg/apis/meta"
    apimeta "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/handler"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)
```

### Types — `artifact.go`

```go
// ArtifactRef carries resolved artifact metadata from a Flux OCIRepository.
// Encapsulates only what downstream phases need, hiding the full Flux object
// (design decision 1: structured result, not raw Flux types).
type ArtifactRef struct {
    // URL is the HTTP(S) address where the artifact can be fetched.
    URL string

    // Revision is the source revision string (e.g., "v0.0.1@sha256:abc...").
    Revision string

    // Digest is the artifact content digest (e.g., "sha256:abc...").
    Digest string
}
```

### Sentinel Errors — `validate.go`

```go
// ErrSourceNotFound indicates the referenced OCIRepository does not exist.
// Callers should classify this as a stalled failure (spec references a non-existent object).
var ErrSourceNotFound = errors.New("source not found")

// ErrSourceNotReady indicates the OCIRepository exists but is not ready.
// Callers should classify this as soft-blocked (waiting for source to become available).
var ErrSourceNotReady = errors.New("source not ready")

// ErrMissingCUEModule (existing) indicates the artifact does not contain a CUE module.
var ErrMissingCUEModule = errors.New("artifact does not contain a cue module")
```

### Functions — `resolve.go`

```go
// Resolve looks up the OCIRepository referenced by sourceRef, validates its
// readiness, and extracts artifact metadata. The OCIRepository is looked up
// in releaseNamespace unless sourceRef.Namespace is set (design decision 2:
// cross-namespace deferred, but sourceRef.Namespace respected if set).
//
// Returns:
//   - *ArtifactRef on success (source ready, artifact present)
//   - error wrapping ErrSourceNotFound if the OCIRepository does not exist
//   - error wrapping ErrSourceNotReady if the source exists but is not ready
//     or has no artifact
func Resolve(
    ctx context.Context,
    c client.Client,
    sourceRef releasesv1alpha1.SourceReference,
    releaseNamespace string,
) (*ArtifactRef, error)
```

### Functions — controller watch (in `modulerelease_controller.go`)

```go
// ociRepositoryToRequests maps an OCIRepository change to all ModuleRelease
// objects that reference it (design decision 3: watch via EnqueueRequestsFromMapFunc).
func (r *ModuleReleaseReconciler) ociRepositoryToRequests(
    ctx context.Context,
    obj client.Object,
) []reconcile.Request

// SetupWithManager — expanded to include:
//   .Watches(&sourcev1.OCIRepository{},
//       handler.EnqueueRequestsFromMapFunc(r.ociRepositoryToRequests))
```
