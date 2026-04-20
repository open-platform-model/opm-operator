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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// ReleaseReconciler reconciles a Release object.
type ReleaseReconciler struct {
	client.Client
	// APIReader is an uncached reader (manager.GetAPIReader()) used for one-off
	// reads that must not provision a cache informer.
	APIReader       client.Reader
	Scheme          *runtime.Scheme
	RestConfig      *rest.Config
	Provider        *provider.Provider
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder

	// Fetcher downloads Flux source artifacts. Injected for testability.
	Fetcher opmsource.Fetcher

	// Renderer loads and renders the CUE release package from the extracted
	// artifact directory. Injected for testability.
	Renderer render.ReleaseRenderer

	// DefaultServiceAccount is the fallback SA name used when a Release has
	// an empty spec.serviceAccountName. Resolved in the release's own
	// namespace. Empty disables the default.
	DefaultServiceAccount string
}

// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=releases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=releases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=releases/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=buckets,verbs=get;list;watch

// Reconcile runs the full Release reconcile loop: source resolution, artifact
// fetch, path navigation, CUE load, kind detection, render, apply, prune, and
// status commit.
func (r *ReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling Release", "name", req.Name, "namespace", req.Namespace)

	return opmreconcile.ReconcileRelease(ctx, &opmreconcile.ReleaseParams{
		Client:                r.Client,
		APIReader:             r.APIReader,
		RestConfig:            r.RestConfig,
		Provider:              r.Provider,
		ResourceManager:       r.ResourceManager,
		EventRecorder:         r.EventRecorder,
		Fetcher:               r.Fetcher,
		Renderer:              r.Renderer,
		DefaultServiceAccount: r.DefaultServiceAccount,
	}, req)
}

// SetupWithManager wires the controller into mgr.
// Watches:
//   - Release CRs (primary, generation-change predicate)
//   - OCIRepository, GitRepository, Bucket (artifact-change predicate, mapped to
//     referencing Releases)
func (r *ReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.Release{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&sourcev1.OCIRepository{},
			handler.EnqueueRequestsFromMapFunc(r.mapSourceToReleases(opmsource.SourceKindOCIRepository)),
			builder.WithPredicates(sourceArtifactChanged{}),
		).
		Watches(
			&sourcev1.GitRepository{},
			handler.EnqueueRequestsFromMapFunc(r.mapSourceToReleases(opmsource.SourceKindGitRepository)),
			builder.WithPredicates(sourceArtifactChanged{}),
		).
		Watches(
			&sourcev1.Bucket{},
			handler.EnqueueRequestsFromMapFunc(r.mapSourceToReleases(opmsource.SourceKindBucket)),
			builder.WithPredicates(sourceArtifactChanged{}),
		).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](1*time.Second, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[ctrl.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		}).
		Named("release").
		Complete(r)
}

// mapSourceToReleases returns a handler that enqueues all Release CRs whose
// spec.sourceRef matches the given source kind and the reconciled object's
// name+namespace.
func (r *ReleaseReconciler) mapSourceToReleases(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list releasesv1alpha1.ReleaseList
		if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for i := range list.Items {
			rel := &list.Items[i]
			if rel.Spec.SourceRef.Kind != kind {
				continue
			}
			if rel.Spec.SourceRef.Name != obj.GetName() {
				continue
			}
			// SourceRef.Namespace defaults to the Release's namespace when empty.
			srcNs := rel.Spec.SourceRef.Namespace
			if srcNs == "" {
				srcNs = rel.Namespace
			}
			if srcNs != obj.GetNamespace() {
				continue
			}
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: rel.Name, Namespace: rel.Namespace},
			})
		}
		return reqs
	}
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
