## Packages: `internal/reconcile` + `internal/controller`

### Files

| File | Purpose |
|------|---------|
| `internal/reconcile/outcome.go` | `Outcome` type and outcome constants (design decision 3: outcome classification) |
| `internal/reconcile/modulerelease.go` | `ReconcileModuleRelease` orchestrator — phases 0-7 (design decisions 1-2) |
| `internal/controller/modulerelease_controller.go` | Expanded `ModuleReleaseReconciler` with dependency fields, wired `Reconcile` body (design decision 1) |
| `internal/reconcile/modulerelease_test.go` | envtest integration tests |

### Imports

```go
// internal/reconcile
import (
    "context"
    "fmt"
    "os"
    "time"

    fluxssa "github.com/fluxcd/pkg/ssa"
    "github.com/fluxcd/pkg/runtime/patch"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    logf "sigs.k8s.io/controller-runtime/pkg/log"

    "github.com/open-platform-model/poc-controller/pkg/core"

    releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
    "github.com/open-platform-model/poc-controller/internal/apply"
    "github.com/open-platform-model/poc-controller/internal/inventory"
    "github.com/open-platform-model/poc-controller/internal/render"
    "github.com/open-platform-model/poc-controller/internal/source"
    "github.com/open-platform-model/poc-controller/internal/status"
)
```

### Types — `internal/reconcile/outcome.go`

```go
// Outcome classifies the result of a reconcile attempt.
// Drives requeue behavior and condition setting (design decision 3).
type Outcome int

const (
    // SoftBlocked — source exists but not ready. Ready=Unknown, Reconciling=True.
    // Requeue: wait for source event or light retry.
    SoftBlocked Outcome = iota

    // NoOp — all four digests match last applied. Ready=True, Reconciling=False.
    // Requeue: none (watch-driven only).
    NoOp

    // Applied — resources applied successfully (no prune needed or prune disabled).
    // Ready=True, Reconciling=False. Requeue: none.
    Applied

    // AppliedAndPruned — resources applied and stale resources pruned.
    // Ready=True, Reconciling=False. Requeue: none.
    AppliedAndPruned

    // FailedTransient — temporary failure (network, API server).
    // Ready=False, Reconciling=True. Requeue: exponential backoff.
    FailedTransient

    // FailedStalled — permanent failure (invalid config, invalid artifact).
    // Ready=False, Stalled=True. Requeue: none (wait for spec/source change).
    FailedStalled
)
```

### Types — `internal/controller/modulerelease_controller.go`

```go
// ModuleReleaseReconciler reconciles a ModuleRelease object.
// Dependencies injected via struct fields (design decision 1).
type ModuleReleaseReconciler struct {
    client.Client
    Scheme *runtime.Scheme

    // SourceResolver resolves OCIRepository references to artifact metadata (change 2).
    // Uses source.Resolve internally.
    SourceResolver func(ctx context.Context, c client.Client, ref releasesv1alpha1.SourceReference, ns string) (*source.ArtifactRef, error)

    // ArtifactFetcher downloads and extracts artifacts (change 3).
    ArtifactFetcher source.Fetcher

    // ResourceManager is the Flux SSA apply engine (change 8).
    ResourceManager *fluxssa.ResourceManager
}
```

### Phase Flow — `internal/reconcile/modulerelease.go`

```go
// ReconcileModuleRelease orchestrates all 8 phases of the reconcile loop.
// Phases run sequentially; errors halt progression (design decision 2).
// Status is always patched at the end via deferred function (design decision 4).
//
// Phase 0: Load ModuleRelease, check deletion, check suspend, create patch helper
// Phase 1: Resolve source via source.Resolve → *source.ArtifactRef
// Phase 2: Fetch artifact via source.Fetcher → extracted dir (temp dir, design decision 5)
// Phase 3: Render via render.RenderModule → *render.RenderResult
// Phase 4: Plan actions — compute DigestSet (change 6), check IsNoOp, compute stale set (change 1)
// Phase 5: Apply via apply.Apply → *apply.ApplyResult
// Phase 6: Prune via apply.Prune → *apply.PruneResult (only if spec.prune && apply succeeded)
// Phase 7: Commit status — conditions (change 7), digests, inventory, history (change 10)
//
// Returns (ctrl.Result, Outcome, error).
func ReconcileModuleRelease(
    ctx context.Context,
    r *ModuleReleaseReconciler,
    req ctrl.Request,
) (ctrl.Result, error)
```

### Outcome → Result Mapping

```go
// Per design decision 3, outcomes map to ctrl.Result:
//
//   SoftBlocked    → ctrl.Result{RequeueAfter: 30s}  (light retry)
//   NoOp           → ctrl.Result{}                    (no requeue)
//   Applied        → ctrl.Result{}                    (no requeue)
//   AppliedAndPruned → ctrl.Result{}                  (no requeue)
//   FailedTransient → ctrl.Result{RequeueAfter: backoff}
//   FailedStalled  → ctrl.Result{}                    (no requeue, wait for watch)
```

### Status Commit Contract

```go
// Phase 7 status commit (design decision 4: deferred patch):
//
// Always updated (even on failure):
//   status.observedGeneration       = mr.Generation
//   status.conditions               = (set by condition helpers, change 7)
//   status.source                   = (from ArtifactRef, if resolved)
//   status.lastAttemptedAction      = "reconcile"
//   status.lastAttemptedAt          = metav1.Now()
//   status.lastAttemptedSourceDigest = digests.Source
//   status.lastAttemptedConfigDigest = digests.Config
//   status.lastAttemptedRenderDigest = digests.Render
//
// Updated ONLY on full success (apply + optional prune):
//   status.lastAppliedAt            = metav1.Now()
//   status.lastAppliedSourceDigest  = digests.Source
//   status.lastAppliedConfigDigest  = digests.Config
//   status.lastAppliedRenderDigest  = digests.Render
//   status.inventory                = new inventory (entries, digest, count, revision+1)
//   status.history                  = prepend success entry (change 10)
//
// Updated on failure:
//   status.failureCounters          = increment relevant counter
//   status.history                  = prepend failure entry (change 10)
```

### Temp Directory Lifecycle

```go
// Design decision 5: temp dir managed by reconcile orchestrator.
//
//   dir, err := os.MkdirTemp("", "opm-artifact-*")
//   if err != nil { ... }
//   defer os.RemoveAll(dir)
//
//   // Phase 2: fetch artifact into dir
//   // Phase 3: render from dir
//   // Cleanup guaranteed even on error or panic.
```
