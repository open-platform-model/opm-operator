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
	"time"

	fluxssa "github.com/fluxcd/pkg/ssa"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/open-platform-model/library/opm/kernel"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
)

// Was: ReleaseReconciler
// ModulePackageReconciler reconciles a ModulePackage object.
type ModulePackageReconciler struct {
	client.Client
	// APIReader is an uncached reader (manager.GetAPIReader()) used for one-off
	// reads that must not provision a cache informer.
	APIReader       client.Reader
	Scheme          *runtime.Scheme
	RestConfig      *rest.Config
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder

	// Fetcher downloads Flux source artifacts. Injected for testability.
	Fetcher opmsource.Fetcher

	// Renderer loads and renders the CUE package from the extracted
	// artifact directory. Injected for testability.
	Renderer render.PackageRenderer

	// DefaultServiceAccount is the fallback SA name used when a ModulePackage has
	// an empty spec.serviceAccountName. Resolved in the ModulePackage's own
	// namespace. Empty disables the default.
	DefaultServiceAccount string

	// Kernel is the shared, long-lived library Kernel constructed once at
	// manager startup. It is the injection seam later enhancement-0001 slices
	// consume to drive the render path; this slice wires it but does not read
	// it on any reconcile path.
	Kernel *kernel.Kernel
}

// +kubebuilder:rbac:groups=opmodel.dev,resources=modulepackages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opmodel.dev,resources=modulepackages/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opmodel.dev,resources=modulepackages/finalizers,verbs=update
// +kubebuilder:rbac:groups=opmodel.dev,resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=buckets,verbs=get;list;watch

// Reconcile runs the full ModulePackage reconcile loop: source resolution, artifact
// fetch, path navigation, CUE load, kind detection, render, apply, prune, and
// status commit.
func (r *ModulePackageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling ModulePackage", "name", req.Name, "namespace", req.Namespace)

	return opmreconcile.ReconcileModulePackage(ctx, &opmreconcile.ModulePackageParams{
		Client:                r.Client,
		APIReader:             r.APIReader,
		RestConfig:            r.RestConfig,
		ResourceManager:       r.ResourceManager,
		EventRecorder:         r.EventRecorder,
		Fetcher:               r.Fetcher,
		Renderer:              r.Renderer,
		DefaultServiceAccount: r.DefaultServiceAccount,
	}, req)
}

// SetupWithManager wires the controller into mgr.
// Watches:
//   - ModulePackage CRs (primary, generation-change predicate)
//   - OCIRepository, GitRepository, Bucket (artifact-change predicate, mapped to
//     referencing ModulePackages) — only when those Flux source CRDs are
//     installed; see fluxSourceCRDsInstalled.
//   - Platform (cluster singleton) — every change re-enqueues all ModulePackages via
//     mapPlatformToModulePackages so packages blocked on PlatformNotReady recover
//     promptly when the platform materializes. The generation predicate lives on
//     For() (not as a global filter) so it does not suppress the Platform watch,
//     whose trigger (materialization) is a status update that does not bump
//     generation.
func (r *ModulePackageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.ModulePackage{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&releasesv1alpha1.Platform{},
			handler.EnqueueRequestsFromMapFunc(r.mapPlatformToModulePackages),
		)

	// The Flux source CRDs are an optional dependency: the ModulePackage source
	// path consumes them, but the ModuleInstance and Platform paths do not.
	// Registering a watch for a Kind whose CRD is absent makes controller-runtime
	// block on an informer cache sync that never completes, so mgr.Start fails
	// after the cache-sync timeout and exits the process — a crashloop that takes
	// the Flux-independent paths down with it. So only wire the source watches
	// when their CRDs are installed; otherwise log and continue. A ModulePackage
	// that references a Flux source will still reconcile and surface the missing
	// dependency on its own status.
	if fluxSourceCRDsInstalled(mgr) {
		b = b.
			Watches(
				&sourcev1.OCIRepository{},
				handler.EnqueueRequestsFromMapFunc(r.mapSourceToModulePackages(opmsource.SourceKindOCIRepository)),
				builder.WithPredicates(sourceArtifactChanged{}),
			).
			Watches(
				&sourcev1.GitRepository{},
				handler.EnqueueRequestsFromMapFunc(r.mapSourceToModulePackages(opmsource.SourceKindGitRepository)),
				builder.WithPredicates(sourceArtifactChanged{}),
			).
			Watches(
				&sourcev1.Bucket{},
				handler.EnqueueRequestsFromMapFunc(r.mapSourceToModulePackages(opmsource.SourceKindBucket)),
				builder.WithPredicates(sourceArtifactChanged{}),
			)
	} else {
		mgr.GetLogger().Info(
			"Flux source CRDs not installed; ModulePackage source watches disabled",
			"controller", "modulepackage",
			"kinds", "OCIRepository,GitRepository,Bucket",
		)
	}

	return b.
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](1*time.Second, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[ctrl.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		}).
		Named("modulepackage").
		Complete(r)
}

// fluxSourceCRDsInstalled reports whether the Flux source CRDs this controller
// watches (OCIRepository, GitRepository, Bucket) are registered in the cluster.
// Flux source-controller installs them together, so any one missing is treated
// as all absent. It probes the manager's RESTMapper, which performs discovery
// and returns a no-match error for an unregistered Kind. The check runs once at
// startup: installing Flux later requires a manager restart to pick up the
// watches, an acceptable trade for not crashlooping when Flux is absent.
func fluxSourceCRDsInstalled(mgr ctrl.Manager) bool {
	mapper := mgr.GetRESTMapper()
	scheme := mgr.GetScheme()
	for _, obj := range []client.Object{
		&sourcev1.OCIRepository{},
		&sourcev1.GitRepository{},
		&sourcev1.Bucket{},
	} {
		gvk, err := apiutil.GVKForObject(obj, scheme)
		if err != nil {
			return false
		}
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			return false
		}
	}
	return true
}

// Was: mapSourceToReleases
// mapSourceToModulePackages returns a handler that enqueues all ModulePackage CRs whose
// spec.sourceRef matches the given source kind and the reconciled object's
// name+namespace.
func (r *ModulePackageReconciler) mapSourceToModulePackages(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list releasesv1alpha1.ModulePackageList
		if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for i := range list.Items {
			pkg := &list.Items[i]
			if pkg.Spec.SourceRef.Kind != kind {
				continue
			}
			if pkg.Spec.SourceRef.Name != obj.GetName() {
				continue
			}
			// SourceRef.Namespace defaults to the ModulePackage's namespace when empty.
			srcNs := pkg.Spec.SourceRef.Namespace
			if srcNs == "" {
				srcNs = pkg.Namespace
			}
			if srcNs != obj.GetNamespace() {
				continue
			}
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: pkg.Name, Namespace: pkg.Namespace},
			})
		}
		return reqs
	}
}

// Was: mapPlatformToReleases
// mapPlatformToModulePackages enqueues every ModulePackage in the cluster when the
// (singleton) Platform changes. This unblocks packages sitting in
// PlatformNotReady the moment the platform materializes, rather than waiting for
// the interval requeue. List-all is cheap: the Platform is a cluster singleton,
// its changes are rare, and the ModulePackage count is bounded.
func (r *ModulePackageReconciler) mapPlatformToModulePackages(ctx context.Context, _ client.Object) []reconcile.Request {
	var list releasesv1alpha1.ModulePackageList
	if err := r.List(ctx, &list); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list ModulePackages for Platform-triggered re-enqueue")
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		pkg := &list.Items[i]
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: pkg.Name, Namespace: pkg.Namespace},
		})
	}
	return reqs
}

// sourceArtifactChanged triggers reconciliation only when a source object's
// artifact revision or digest changes. Drops updates that only touch spec or
// unrelated status fields.
type sourceArtifactChanged struct {
	predicate.Funcs
}

func (sourceArtifactChanged) Update(e event.UpdateEvent) bool {
	oldArt := artifactOf(e.ObjectOld)
	newArt := artifactOf(e.ObjectNew)
	if oldArt == nil && newArt == nil {
		return false
	}
	if oldArt == nil || newArt == nil {
		return true
	}
	return oldArt.Revision != newArt.Revision || oldArt.Digest != newArt.Digest
}

func (sourceArtifactChanged) Create(_ event.CreateEvent) bool { return true }
func (sourceArtifactChanged) Delete(_ event.DeleteEvent) bool { return false }

// artifactOf returns the revision/digest for a supported Flux source object,
// or nil if the object type is unknown or has no artifact.
func artifactOf(obj client.Object) *artifactRef {
	switch s := obj.(type) {
	case *sourcev1.OCIRepository:
		if a := s.GetArtifact(); a != nil {
			return &artifactRef{Revision: a.Revision, Digest: a.Digest}
		}
	case *sourcev1.GitRepository:
		if a := s.GetArtifact(); a != nil {
			return &artifactRef{Revision: a.Revision, Digest: a.Digest}
		}
	case *sourcev1.Bucket:
		if a := s.GetArtifact(); a != nil {
			return &artifactRef{Revision: a.Revision, Digest: a.Digest}
		}
	}
	return nil
}

// artifactRef is a local snapshot of the artifact fields the predicate compares.
type artifactRef struct {
	Revision string
	Digest   string
}
