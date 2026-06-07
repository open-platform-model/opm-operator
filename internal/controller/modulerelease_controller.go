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
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// ModuleReleaseReconciler reconciles a ModuleRelease object.
// Dependencies are injected via struct fields at manager setup time.
type ModuleReleaseReconciler struct {
	client.Client
	// APIReader is an uncached reader (manager.GetAPIReader()) used for one-off
	// reads that must not provision a cache informer.
	APIReader       client.Reader
	Scheme          *runtime.Scheme
	RestConfig      *rest.Config
	Provider        *provider.Provider
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder
	Renderer        render.ModuleRenderer
	// DefaultServiceAccount is the fallback SA name used when a
	// ModuleRelease has an empty spec.serviceAccountName. Resolved in the
	// release's own namespace. Empty disables the default.
	DefaultServiceAccount string
	// Kernel is the shared, long-lived library Kernel constructed once at
	// manager startup. It is the injection seam later enhancement-0001 slices
	// consume to drive the render path; this slice wires it but does not read
	// it on any reconcile path.
	Kernel *kernel.Kernel
}

// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases/finalizers,verbs=update
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;impersonate
// +kubebuilder:rbac:groups="",resources=users;groups,verbs=impersonate
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update

// Reconcile runs the full ModuleRelease reconcile loop: CUE module synthesis
// and resolution from OCI registry, rendering, SSA apply, optional prune,
// and status commit.
func (r *ModuleReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling ModuleRelease", "name", req.Name, "namespace", req.Namespace)

	return opmreconcile.ReconcileModuleRelease(ctx, &opmreconcile.ModuleReleaseParams{
		Client:                r.Client,
		APIReader:             r.APIReader,
		RestConfig:            r.RestConfig,
		Provider:              r.Provider,
		ResourceManager:       r.ResourceManager,
		EventRecorder:         r.EventRecorder,
		Renderer:              r.Renderer,
		DefaultServiceAccount: r.DefaultServiceAccount,
	}, req)
}

// SetupWithManager sets up the controller with the Manager.
//
// Watches:
//   - ModuleRelease CRs (primary, generation-change predicate)
//   - Platform (cluster singleton) — every change re-enqueues all
//     ModuleReleases via mapPlatformToModuleReleases so releases blocked on
//     PlatformNotReady recover promptly when the platform materializes. The
//     generation predicate lives on For() (not as a global event filter) so it
//     does not suppress the Platform watch, whose trigger (materialization) is
//     a status update that does not bump generation.
func (r *ModuleReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.ModuleRelease{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&releasesv1alpha1.Platform{},
			handler.EnqueueRequestsFromMapFunc(r.mapPlatformToModuleReleases),
		).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](1*time.Second, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[ctrl.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		}).
		Named("modulerelease").
		Complete(r)
}

// mapPlatformToModuleReleases enqueues every ModuleRelease in the cluster when
// the (singleton) Platform changes. This unblocks releases sitting in
// PlatformNotReady the moment the platform materializes, rather than waiting for
// the stalled-recheck backoff. List-all is cheap: the Platform is a cluster
// singleton, its changes are rare, and the release count is bounded.
func (r *ModuleReleaseReconciler) mapPlatformToModuleReleases(ctx context.Context, _ client.Object) []reconcile.Request {
	var list releasesv1alpha1.ModuleReleaseList
	if err := r.List(ctx, &list); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list ModuleReleases for Platform-triggered re-enqueue")
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		mr := &list.Items[i]
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: mr.Name, Namespace: mr.Namespace},
		})
	}
	return reqs
}
