/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"time"

	"github.com/fluxcd/pkg/runtime/patch"
	loaderfile "github.com/open-platform-model/library/opm/helper/loader/file"
	"github.com/open-platform-model/library/opm/kernel"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/platformmodule"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/internal/version"
)

// platformSingletonName is the only permitted name for the cluster-scoped
// Platform singleton. The CRD enforces this via a CEL rule; the reconciler
// guards on it again as defense-in-depth (enhancement 0001 §8.1).
const platformSingletonName = "cluster"

// transientRecheckInterval is the fast retry cadence for clearly-transient
// build failures (network/timeout). Kept conservative (a minute, not
// seconds) so a transient registry blip self-heals quickly without hammering
// the singleton's registry; non-transient and unclassifiable failures fall
// back to the long reconcile.StalledRecheckInterval.
const transientRecheckInterval = time.Minute

// PlatformReconciler reconciles the singleton Platform CR into a platform CUE
// module on the operator's own disk (enhancement 0019 D6). Per CR generation
// it derives the module's dependency closure from the pinned catalogs'
// published module files, generates the module (one importing #registry
// entry per subscription, the CR's version stamped as the expected-version
// tripwire), writes it under a per-generation directory, builds it through
// the kernel's shape-gated platform loader, and records the result together
// with the resolved skew policy (spec.skewPolicy, 0019 D7/D18) in the
// process-local store for the render path. The outcome surfaces on the CR's
// Ready condition: Generated, GenerateFailed or BuildFailed.
type PlatformReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder events.EventRecorder

	// Kernel is the shared, long-lived library Kernel constructed once at
	// manager startup. The platform build runs on it.
	Kernel *kernel.Kernel

	// Store holds the current generated platform. Written here, read by the
	// render path.
	Store *platformstore.Store

	// Registry is the CUE registry mapping (the manager's --registry value)
	// the closure derivation and the build resolve through. Empty falls back
	// to the process CUE_REGISTRY.
	Registry string

	// Layout owns the module directories under the manager's --platform-dir.
	Layout platformmodule.Layout

	// ModFiles serves published module files for the closure derivation.
	// Nil constructs one from Registry on first use; a test may inject a
	// fixture graph.
	ModFiles platformmodule.ModFileSource
}

// +kubebuilder:rbac:groups=opmodel.dev,resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups=opmodel.dev,resources=platforms/status,verbs=get;update;patch

// Reconcile generates and builds the platform module for the
// cluster-singleton Platform and records the outcome on its status. It
// reconciles only the object named "cluster"; any other name is ignored
// without error. On delete it clears the store (workloads are untouched:
// §8.4 freeze-don't-teardown); the module directories are left for the next
// generation's prune or the next manager start.
func (r *PlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Defense-in-depth: only the singleton is reconciled.
	if req.Name != platformSingletonName {
		log.V(1).Info("Ignoring non-singleton Platform", "name", req.Name)
		return ctrl.Result{}, nil
	}

	var plat releasesv1alpha1.Platform
	if err := r.Get(ctx, req.NamespacedName, &plat); err != nil {
		if apierrors.IsNotFound(err) {
			// Deleted: drop the held platform. Workloads are not torn down.
			r.Store.Clear()
			log.Info("Platform deleted, cleared platform store", "name", req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Mid-deletion (object still present, e.g. a foreign finalizer): clear the
	// slot now so readers see no platform.
	if !plat.DeletionTimestamp.IsZero() {
		r.Store.Clear()
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling Platform", "name", plat.Name, "generation", plat.Generation)

	// Snapshot before mutation so the serial patcher diffs against the
	// pre-reconcile status.
	patcher := patch.NewSerialPatcher(&plat, r.Client)

	entries, err := platformEntries(&plat)
	if err != nil {
		// A stored object predating the CRD-required version field. Nothing
		// external can change this; the stalled recheck keeps the status
		// honest without hammering anything.
		return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, err.Error())
	}

	src, err := r.modFiles()
	if err != nil {
		return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, fmt.Sprintf("configuring module registry: %v", err))
	}
	deps, err := platformmodule.Closure(ctx, src, platformmodule.Roots(entries))
	if err != nil {
		// The pinned build does not exist, or the registry is unreachable:
		// the error names the module path and version.
		return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, fmt.Sprintf("resolving platform dependencies: %v", err))
	}

	files, err := platformmodule.Generate(platformmodule.Input{
		Name:    plat.Name,
		Type:    plat.Spec.Type,
		Entries: entries,
		Deps:    deps,
	})
	if err != nil {
		return r.failReconcile(ctx, patcher, &plat, status.GenerateFailedReason, err, fmt.Sprintf("generating platform module: %v", err))
	}

	// The kernel gate serialises the build against the render paths' own
	// context-owning calls (acquisition, synthesis): the build evaluates in
	// the shared Kernel's context (see Store.AcquireKernel). Taken before the
	// write and held through Store.SetGenerated and the prune so a
	// same-generation rewrite, which moves the live directory aside during
	// the swap, is never observed by an acquisition mid-flight. Render builds
	// run outside the gate and are protected by their lease instead: the
	// prune keep set below covers every leased generation. The registry I/O
	// (the closure derivation above) stays outside the gate.
	release := r.Store.AcquireKernel()
	defer release()

	dir, err := r.Layout.Write(plat.Generation, files)
	if err != nil {
		return r.failReconcile(ctx, patcher, &plat, status.GenerateFailedReason, err, fmt.Sprintf("writing platform module: %v", err))
	}

	// The build is the validation: it proves the pins resolve and exercises
	// the schema's own tripwires (stamped-versus-derived version, key-to-
	// modulePath binding), which name the offending #registry entry. The
	// source-carrying acquisition is what the render build imports the
	// platform from (Kernel.Render requires Platform.Source).
	p, err := r.Kernel.AcquirePlatformFromDir(ctx, dir, loaderfile.LoadOptions{Registry: r.Registry})
	if err != nil {
		return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, fmt.Sprintf("building platform module: %v", err))
	}

	// Success: record the generated module under the generation key with the
	// resolved skew policy, then prune every directory no render can still
	// be reading: keep the current generation plus every generation a render
	// holds a lease on (exact, replacing the "current plus previous"
	// approximation). A generation leased now is pruned by the next reconcile
	// once released.
	r.Store.SetGenerated(platformstore.Generated{
		Generation: plat.Generation,
		Dir:        dir,
		Platform:   p,
		Skew:       skewPolicy(&plat),
	})
	keep := append([]int64{plat.Generation}, r.Store.Leased()...)
	if err := r.Layout.Prune(keep...); err != nil {
		log.Error(err, "Failed to prune superseded platform modules", "dir", r.Layout.Root)
	}

	plat.Status.ObservedGeneration = plat.Generation
	plat.Status.OperatorVersion = version.Full()
	status.MarkReadyWithReason(&plat, status.GeneratedReason, "Platform module generated and built for generation %d", plat.Generation)
	r.EventRecorder.Eventf(&plat, nil, corev1.EventTypeNormal, status.GeneratedReason, "Generate", "Platform module generated and built for generation %d", plat.Generation)

	log.Info("Platform module generated and built", "name", plat.Name, "generation", plat.Generation, "dir", dir)
	return ctrl.Result{}, r.patchStatus(ctx, patcher, &plat)
}

// modFiles returns the module-file source for closure derivation,
// constructing it from Registry on first use.
func (r *PlatformReconciler) modFiles() (platformmodule.ModFileSource, error) {
	if r.ModFiles != nil {
		return r.ModFiles, nil
	}
	src, err := platformmodule.NewRegistry(r.Registry)
	if err != nil {
		return nil, err
	}
	r.ModFiles = src
	return src, nil
}

// failReconcile records a generate/build failure on plat and returns the
// requeue result. Both resolve against mutable external state (a registry,
// a volume), so no failure is terminal: it sets Ready=False with reason and
// msg, records observedGeneration (so a stalled Platform reflects the
// generation it observed rather than reading as un-reconciled), and requeues
// on a bounded interval: short for clearly-transient causes (classified
// best-effort from classifyErr), the long stalled recheck otherwise. The
// warning event is emitted only when the failure is newly entered or its
// reason or message changes, so periodic rechecks of an unchanged failure do
// not spam events. The store is left untouched, preserving any last-good
// generated platform.
func (r *PlatformReconciler) failReconcile(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	plat *releasesv1alpha1.Platform,
	reason string,
	classifyErr error,
	msg string,
) (ctrl.Result, error) {
	// Capture the pre-mutation Ready condition to gate the event on transition.
	prior := apimeta.FindStatusCondition(plat.Status.Conditions, status.ReadyCondition)
	transition := prior == nil ||
		prior.Status != metav1.ConditionFalse ||
		prior.Reason != reason ||
		prior.Message != msg

	plat.Status.ObservedGeneration = plat.Generation
	plat.Status.OperatorVersion = version.Full()
	status.MarkStalled(plat, reason, "%s", msg)

	if transition {
		r.EventRecorder.Eventf(plat, nil, corev1.EventTypeWarning, reason, "Generate", "%s", msg)
	}

	interval := opmreconcile.StalledRecheckInterval
	if isTransientFailure(classifyErr) {
		interval = transientRecheckInterval
	}
	return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, patcher, plat)
}

// isTransientFailure reports whether err (or any error it wraps) is a
// clearly-transient network/timeout failure worth a fast retry. It is
// best-effort: unrecognized causes return false so the caller falls back to the
// long recheck interval, making a misclassification never worse than a slow
// recheck.
func isTransientFailure(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return true
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// patchStatus commits the Platform status via the serial patcher, declaring the
// Ready/Reconciling/Stalled conditions this controller owns.
func (r *PlatformReconciler) patchStatus(ctx context.Context, patcher *patch.SerialPatcher, plat *releasesv1alpha1.Platform) error {
	return patcher.Patch(ctx, plat,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
			},
		},
	)
}

// skewPolicy resolves spec.skewPolicy to the kernel's policy: Refuse maps to
// SkewRefuse, anything else (Warn, unset) to SkewWarn, the D18 default. The
// CRD enum keeps other values out at admission.
func skewPolicy(plat *releasesv1alpha1.Platform) kernel.SkewPolicy {
	if plat.Spec.SkewPolicy != nil && *plat.Spec.SkewPolicy == releasesv1alpha1.SkewPolicyRefuse {
		return kernel.SkewRefuse
	}
	return kernel.SkewWarn
}

// platformEntries maps the CR's registry to the generator's entries, in
// sorted path order. The CRD was authored as a 1:1 projection of the core
// #Platform surface, so the mapping is mechanical: a nil Enable resolves to
// the schema default (true). A subscription without a version (a stored
// object predating the CRD-required field, which validation ratcheting keeps
// status-patchable) is refused naming the path.
func platformEntries(plat *releasesv1alpha1.Platform) ([]platformmodule.Entry, error) {
	entries := make([]platformmodule.Entry, 0, len(plat.Spec.Registry))
	for path, sub := range plat.Spec.Registry {
		if sub.Version == "" {
			return nil, fmt.Errorf("registry entry %q: version is required (stored object predates the required field)", path)
		}
		entries = append(entries, platformmodule.Entry{
			Path:    path,
			Version: sub.Version,
			Enable:  sub.Enable == nil || *sub.Enable,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// SetupWithManager wires the controller into mgr, watching the Platform
// singleton with a generation-change predicate.
func (r *PlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.Platform{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("platform").
		Complete(r)
}
