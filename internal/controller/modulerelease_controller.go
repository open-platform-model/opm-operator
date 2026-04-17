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
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/render"
	"github.com/open-platform-model/poc-controller/pkg/provider"
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
}

// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;impersonate
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update

// Reconcile runs the full ModuleRelease reconcile loop: CUE module synthesis
// and resolution from OCI registry, rendering, SSA apply, optional prune,
// and status commit.
func (r *ModuleReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling ModuleRelease", "name", req.Name, "namespace", req.Namespace)

	return opmreconcile.ReconcileModuleRelease(ctx, &opmreconcile.ModuleReleaseParams{
		Client:          r.Client,
		APIReader:       r.APIReader,
		RestConfig:      r.RestConfig,
		Provider:        r.Provider,
		ResourceManager: r.ResourceManager,
		EventRecorder:   r.EventRecorder,
		Renderer:        r.Renderer,
	}, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.ModuleRelease{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](1*time.Second, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[ctrl.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		}).
		Named("modulerelease").
		Complete(r)
}
