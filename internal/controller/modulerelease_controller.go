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

	fluxssa "github.com/fluxcd/pkg/ssa"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/source"
	"github.com/open-platform-model/poc-controller/pkg/provider"
)

// ModuleReleaseReconciler reconciles a ModuleRelease object.
// Dependencies are injected via struct fields at manager setup time.
type ModuleReleaseReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Provider        *provider.Provider
	ResourceManager *fluxssa.ResourceManager
	ArtifactFetcher source.Fetcher
}

// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=modulereleases/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories/status,verbs=get

// Reconcile runs the full ModuleRelease reconcile loop: source resolution,
// artifact fetch, CUE rendering, SSA apply, optional prune, and status commit.
func (r *ModuleReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling ModuleRelease", "name", req.Name, "namespace", req.Namespace)

	return opmreconcile.ReconcileModuleRelease(ctx, &opmreconcile.ModuleReleaseParams{
		Client:          r.Client,
		Provider:        r.Provider,
		ResourceManager: r.ResourceManager,
		ArtifactFetcher: r.ArtifactFetcher,
	}, req)
}

// ociRepositoryToRequests maps an OCIRepository change to all ModuleRelease
// objects that reference it.
func (r *ModuleReleaseReconciler) ociRepositoryToRequests(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var releases releasesv1alpha1.ModuleReleaseList
	if err := r.List(ctx, &releases, client.InNamespace(obj.GetNamespace())); err != nil {
		log.Error(err, "Failed to list ModuleReleases for OCIRepository mapping")
		return nil
	}

	var requests []reconcile.Request
	for _, mr := range releases.Items {
		ref := mr.Spec.SourceRef
		ns := ref.Namespace
		if ns == "" {
			ns = mr.Namespace
		}
		if ref.Name == obj.GetName() && ns == obj.GetNamespace() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      mr.Name,
					Namespace: mr.Namespace,
				},
			})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.ModuleRelease{}).
		Watches(&sourcev1.OCIRepository{},
			handler.EnqueueRequestsFromMapFunc(r.ociRepositoryToRequests)).
		Named("modulerelease").
		Complete(r)
}
