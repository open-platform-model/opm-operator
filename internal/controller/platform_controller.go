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
	"os"
	"sort"
	"time"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/open-platform-model/library/opm/helper/platformmodule"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/internal/version"
)

// platformSingletonName is the only permitted name for the cluster-scoped
// Platform singleton. The CRD enforces this via a CEL rule; the reconciler
// guards on it again as defense-in-depth (enhancement 0001 §8.1).
const platformSingletonName = "cluster"

// PlatformModulePath is the generated platform module's own identity: the
// reserved, never-published platforms namespace (enhancement 0019 D6). Fixed
// rather than derived per generation so generated files are byte-stable
// across generations of the same spec, and distinct from every instance
// module path the render build could pair it with. Operator input to the
// library's generator, which owns everything else about the module
// including the core pin (its verified release, schema.DefaultSchemaVersion).
const PlatformModulePath = "opmodel.dev/platforms/cluster@v0"

// transientRecheckInterval is the fast retry cadence for clearly-transient
// build failures (network/timeout). Kept conservative (a minute, not
// seconds) so a transient registry blip self-heals quickly without hammering
// the singleton's registry; non-transient and unclassifiable failures fall
// back to the long reconcile.StalledRecheckInterval.
const transientRecheckInterval = time.Minute

// PlatformReconciler reconciles the singleton Platform CR into a platform CUE
// module on the operator's own disk (enhancement 0019 D6). Per CR generation
// it derives the module's dependency closure from the pinned catalogs'
// published module files and generates the module through the library's
// platform-module helper (one importing #registry entry per subscription,
// the CR's version stamped as the expected-version tripwire, core pinned at
// the library's verified release), writes it under a per-generation
// directory, builds it through the kernel's shape-gated platform loader, and
// records the result together with the resolved skew policy
// (spec.skewPolicy, 0019 D7/D18) in the process-local store for the render
// path. The outcome surfaces on the CR's Ready condition: Generated,
// GenerateFailed or BuildFailed.
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
	// the closure derivation resolves through; the build resolves through
	// the mapping Kernel was constructed with. Empty falls back to the
	// process CUE_REGISTRY.
	Registry string

	// Layout owns the module directories under the manager's --platform-dir.
	Layout platformstore.Layout

	// ModFiles serves published module files for the closure derivation.
	// Nil constructs one from Registry on first use; a test may inject a
	// fixture graph.
	ModFiles platformmodule.ModFileSource
}

// +kubebuilder:rbac:groups=opmodel.dev,resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups=opmodel.dev,resources=platforms/status,verbs=get;update;patch

// TransformerRegistration claims are the platform's second transformer path
// (enhancement 0015 D3), so the reconciler that builds the platform module
// reads them and reports on them. Read and status verbs only: the operator
// judges claims, it never creates one. Creating a claim is platform-admin
// RBAC (config/rbac/transformerregistration_admin_role.yaml).
// +kubebuilder:rbac:groups=opmodel.dev,resources=transformerregistrations,verbs=get;list;watch
// +kubebuilder:rbac:groups=opmodel.dev,resources=transformerregistrations/status,verbs=get;update;patch

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

	// The generated package is a function of exactly one tuple (enhancement
	// 0015 D13): this CR's spec and the set of accepted-and-active claims,
	// both read here as current state. Nothing is read from the event that
	// woke the reconcile, so a stale, duplicated or reordered event yields
	// the package the current state implies, and a burst of activations
	// converges instead of producing one package per claim.
	claims, err := r.activeClaims(ctx)
	if err != nil {
		// A transient read against the API server says nothing about the
		// platform: retry with the controller's backoff rather than writing a
		// verdict the next list would contradict.
		return ctrl.Result{}, err
	}

	// The identity of the package this reconcile will generate, computed
	// before any work so the write, the store record and the prune keep set
	// all name the same package.
	identity := platformstore.NewPackageIdentity(plat.Generation, claimCoordinates(claims))

	entries, err := platformEntries(&plat, claims)
	if err != nil {
		// A stored object predating the CRD-required version field. Nothing
		// external can change this; the stalled recheck keeps the status
		// honest without hammering anything.
		return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, err.Error())
	}

	// Level-computed generation makes a repeat of the same tuple a no-op: the
	// package held is the package this reconcile would produce, byte for
	// byte. Skipping it is what bounds a burst of claim activations to one
	// build rather than one per claim. The directory is checked because the
	// render path reads it, so a record whose module is gone must be rebuilt.
	if held, ok := r.Store.Generated(); ok && held.Identity == identity && dirExists(held.Dir) {
		log.V(1).Info("Platform module already current, skipping regeneration",
			"name", plat.Name, "identity", identity, "dir", held.Dir)
		plat.Status.ObservedGeneration = plat.Generation
		plat.Status.OperatorVersion = version.Full()
		recordEffectiveRegistry(&plat, identity, entries)
		status.MarkReadyWithReason(&plat, status.GeneratedReason, "Platform module generated and built for generation %d", plat.Generation)
		return ctrl.Result{}, r.patchStatus(ctx, patcher, &plat)
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
		Name:       plat.Name,
		Type:       plat.Spec.Type,
		ModulePath: PlatformModulePath,
		Entries:    entries,
		Deps:       deps,
	})
	if err != nil {
		return r.failReconcile(ctx, patcher, &plat, status.GenerateFailedReason, err, fmt.Sprintf("generating platform module: %v", err))
	}

	// No kernel gate: every Kernel verb builds in a context of its own
	// (library ADR-007), so the build below runs concurrently with the render
	// paths' acquisitions and syntheses. Render builds read the module
	// directory under their lease: the prune keep set below covers every
	// leased identity.

	dir, err := r.Layout.Write(identity, files)
	if err != nil {
		return r.failReconcile(ctx, patcher, &plat, status.GenerateFailedReason, err, fmt.Sprintf("writing platform module: %v", err))
	}

	// The build is the validation: it proves the pins resolve and exercises
	// the schema's own tripwires (stamped-versus-derived version, key-to-
	// modulePath binding), which name the offending #registry entry. The
	// source-carrying acquisition is what the render build imports the
	// platform from (Kernel.Render requires Platform.Source).
	p, err := r.Kernel.AcquirePlatformFromDir(ctx, dir)
	if err != nil {
		return r.failReconcile(ctx, patcher, &plat, status.BuildFailedReason, err, fmt.Sprintf("building platform module: %v", err))
	}

	// Success: record the generated module under its identity with the
	// resolved skew policy, then prune every directory no render can still
	// be reading: keep the current identity plus every identity a render
	// holds a lease on (exact, replacing the "current plus previous"
	// approximation). A package leased now is pruned by the next reconcile
	// once released.
	r.Store.SetGenerated(platformstore.Generated{
		Identity: identity,
		Dir:      dir,
		Platform: p,
		Skew:     skewPolicy(&plat),
	})
	keep := append([]platformstore.PackageIdentity{identity}, r.Store.Leased()...)
	if err := r.Layout.Prune(keep...); err != nil {
		log.Error(err, "Failed to prune superseded platform modules", "dir", r.Layout.Root)
	}

	plat.Status.ObservedGeneration = plat.Generation
	plat.Status.OperatorVersion = version.Full()
	recordEffectiveRegistry(&plat, identity, entries)
	status.MarkReadyWithReason(&plat, status.GeneratedReason, "Platform module generated and built for generation %d", plat.Generation)
	r.EventRecorder.Eventf(&plat, nil, corev1.EventTypeNormal, status.GeneratedReason, "Generate", "Platform module generated and built for generation %d", plat.Generation)

	log.Info("Platform module generated and built",
		"name", plat.Name, "generation", plat.Generation, "identity", identity, "activeClaims", len(claims), "dir", dir)
	return ctrl.Result{}, r.patchStatus(ctx, patcher, &plat)
}

// activeClaims returns the TransformerRegistrations that are both accepted
// and active, in name order, which is the half of the tuple the Platform CR
// does not carry. Judging claims is not this reconciler's job: the claim
// reconciler owns acceptance and activation and this only consumes the
// verdict (design.md § the claim reconciler stays the judge).
//
// A claim being deleted is dropped only once its removal guard has released
// it (enhancement 0015 D3). A deletion timestamp alone does not drop it: the
// guard blocks removal precisely while instances are still rendering against
// that catalog, so dropping it here would take the catalog out of the next
// generated package and abandon those instances through the door the block
// was closing — with the claim still reporting that it was protecting them.
// Once the finalizer is gone nothing is depending on the claim any more and
// it leaves the set, which is what it has always done.
func (r *PlatformReconciler) activeClaims(ctx context.Context) ([]releasesv1alpha1.TransformerRegistration, error) {
	var list releasesv1alpha1.TransformerRegistrationList
	if err := r.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("listing transformer registrations: %w", err)
	}
	active := make([]releasesv1alpha1.TransformerRegistration, 0, len(list.Items))
	for _, claim := range list.Items {
		if !claim.Status.Accepted || !claim.Status.Active {
			continue
		}
		if !claim.DeletionTimestamp.IsZero() && !claimContributesAfterDeletion(&claim) {
			continue
		}
		active = append(active, claim)
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Name < active[j].Name })
	return active, nil
}

// claimCoordinates maps active claims to the catalog coordinates the package
// identity is built from.
func claimCoordinates(claims []releasesv1alpha1.TransformerRegistration) []platformstore.ClaimCoordinate {
	coords := make([]platformstore.ClaimCoordinate, 0, len(claims))
	for _, claim := range claims {
		coords = append(coords, platformstore.ClaimCoordinate{
			Catalog: claim.Spec.Catalog,
			Version: claim.Spec.Version,
		})
	}
	return coords
}

// recordEffectiveRegistry stamps the package identity and the resolved
// registry union on the Platform's status. It is called only where a package
// under that identity is the one the store holds: after a successful build,
// and on the no-op skip. A failed generation leaves both fields describing the
// last-good package, for the same reason failReconcile leaves the store
// untouched — status would otherwise advertise a registry the operator never
// built and no render can be consuming.
func recordEffectiveRegistry(
	plat *releasesv1alpha1.Platform,
	identity platformstore.PackageIdentity,
	entries []platformmodule.Entry,
) {
	plat.Status.PackageIdentity = identity.String()
	plat.Status.Registry = resolvedRegistry(plat, entries)
}

// resolvedRegistry maps the generator's entries to the union Platform status
// carries: every catalog the generated module pins and imports, with the path
// it took to get there. An entry whose catalog the CR's spec.registry names
// is an authored subscription; every other entry came from an active claim,
// which is exactly how platformEntries folded them in.
func resolvedRegistry(plat *releasesv1alpha1.Platform, entries []platformmodule.Entry) []releasesv1alpha1.ResolvedRegistryEntry {
	resolved := make([]releasesv1alpha1.ResolvedRegistryEntry, 0, len(entries))
	for _, entry := range entries {
		source := releasesv1alpha1.RegistryEntryRegistration
		if _, authored := plat.Spec.Registry[entry.Path]; authored {
			source = releasesv1alpha1.RegistryEntrySubscription
		}
		resolved = append(resolved, releasesv1alpha1.ResolvedRegistryEntry{
			Catalog: entry.Path,
			Version: entry.Version,
			Enabled: entry.Enable,
			Source:  source,
		})
	}
	return resolved
}

// dirExists reports whether path is an existing directory. The store's record
// names a module directory the render path reads, so a record whose directory
// has gone (an ephemeral volume replaced under a running manager) must not be
// treated as current.
func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// modFiles returns the module-file source for closure derivation,
// constructing it on first use from the operator's registry mapping, its
// client type and the process environment (CUE_CACHE_DIR is set at manager
// start), passed explicitly: the helper reads nothing from the process.
func (r *PlatformReconciler) modFiles() (platformmodule.ModFileSource, error) {
	if r.ModFiles != nil {
		return r.ModFiles, nil
	}
	src, err := platformmodule.NewRegistry(platformmodule.RegistryConfig{
		Registry:   r.Registry,
		ClientType: "opm-operator",
		Env:        os.Environ(),
	})
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

// platformEntries maps the tuple to the generator's entries, in sorted path
// order: one entry per subscription the CR authored, plus one per active
// claim, so the provider catalogs a render needs are imported beside the
// subscribed ones (enhancement 0015 D13).
//
// The CRD was authored as a 1:1 projection of the core #Platform surface, so
// the subscription mapping is mechanical: a nil Enable resolves to the schema
// default (true). A subscription without a version (a stored object predating
// the CRD-required field, which validation ratcheting keeps status-patchable)
// is refused naming the path.
//
// A catalog path contributes exactly one entry, because it is one #registry
// key. An authored subscription wins over a claim naming the same catalog:
// the platform admin's pin and enable decision is the deliberate one, and a
// disabled subscription must not be re-enabled by a provider registering
// against it. Claims are consumed in the name order activeClaims sorted them
// into, so two claims naming one catalog resolve deterministically; the claim
// reconciler is what keeps that pair from arising (D2, D12).
func platformEntries(plat *releasesv1alpha1.Platform, claims []releasesv1alpha1.TransformerRegistration) ([]platformmodule.Entry, error) {
	entries := make([]platformmodule.Entry, 0, len(plat.Spec.Registry)+len(claims))
	seen := make(map[string]bool, len(plat.Spec.Registry)+len(claims))
	for path, sub := range plat.Spec.Registry {
		if sub.Version == "" {
			return nil, fmt.Errorf("registry entry %q: version is required (stored object predates the required field)", path)
		}
		entries = append(entries, platformmodule.Entry{
			Path:    path,
			Version: sub.Version,
			Enable:  sub.Enable == nil || *sub.Enable,
		})
		seen[path] = true
	}
	for _, claim := range claims {
		if seen[claim.Spec.Catalog] {
			continue
		}
		entries = append(entries, platformmodule.Entry{
			Path:    claim.Spec.Catalog,
			Version: claim.Spec.Version,
			Enable:  true,
		})
		seen[claim.Spec.Catalog] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// SetupWithManager wires the controller into mgr, watching both inputs of the
// tuple the generated package is a function of: the Platform singleton under
// a generation-change predicate, and every TransformerRegistration whose
// contribution to the active set changes, so a claim activating regenerates
// the platform without the CR being edited (enhancement 0015 D13).
func (r *PlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.Platform{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&releasesv1alpha1.TransformerRegistration{},
			handler.EnqueueRequestsFromMapFunc(mapClaimToPlatform),
			builder.WithPredicates(claimContributionPredicate()),
		).
		Named("platform").
		Complete(r)
}

// mapClaimToPlatform enqueues the singleton whatever claim changed. The
// reconcile recomputes the whole tuple from current state, so the event is a
// wake-up and never an input: which claim woke it, and what it said, are
// deliberately discarded.
func mapClaimToPlatform(_ context.Context, _ client.Object) []ctrl.Request {
	return []ctrl.Request{{NamespacedName: client.ObjectKey{Name: platformSingletonName}}}
}

// claimContribution returns the coordinate a claim contributes to the active
// set, and whether it contributes at all. A claim only contributes once the
// claim reconciler has both accepted it and found its provider serving, and
// it keeps contributing through a blocked deletion for as long as
// activeClaims keeps it — the two read the same rule, so the predicate never
// suppresses an event the reconcile would have acted on, nor wakes one it
// would not.
//
// The practical effect of the deletion clause is that stamping a deletion
// timestamp wakes nothing (the claim still contributes, so nothing moved),
// and the guard releasing it does (the contribution stops). That is the
// correct pair of edges: the package should change when the provider actually
// leaves, not when someone asks it to.
func claimContribution(obj client.Object) (platformstore.ClaimCoordinate, bool) {
	claim, ok := obj.(*releasesv1alpha1.TransformerRegistration)
	if !ok || !claim.Status.Accepted || !claim.Status.Active {
		return platformstore.ClaimCoordinate{}, false
	}
	if !claim.DeletionTimestamp.IsZero() && !claimContributesAfterDeletion(claim) {
		return platformstore.ClaimCoordinate{}, false
	}
	return platformstore.ClaimCoordinate{Catalog: claim.Spec.Catalog, Version: claim.Spec.Version}, true
}

// claimContributionPredicate passes only the claim events that can move the
// active set: a claim arriving or leaving while contributing, and an update
// that starts, stops or repoints a contribution. Everything else — an
// un-judged claim being stored, a condition message changing, a cache resync
// of a claim that contributes nothing — would wake a reconcile that computes
// the identity it already holds, so it is dropped here rather than absorbed
// by the no-op skip.
func claimContributionPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			_, contributes := claimContribution(e.Object)
			return contributes
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			_, contributed := claimContribution(e.Object)
			return contributed
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			was, wasContributing := claimContribution(e.ObjectOld)
			is, isContributing := claimContribution(e.ObjectNew)
			return wasContributing != isContributing || was != is
		},
	}
}
