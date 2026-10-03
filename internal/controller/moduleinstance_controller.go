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
	"fmt"
	"time"

	fluxssa "github.com/fluxcd/pkg/ssa"
	"github.com/open-platform-model/library/opm/kernel"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/open-platform-model/opm-operator/internal/status"
)

// ModuleInstanceReconciler reconciles a ModuleInstance object.
// Dependencies are injected via struct fields at manager setup time.
type ModuleInstanceReconciler struct {
	client.Client
	// APIReader is an uncached reader (manager.GetAPIReader()) used for one-off
	// reads that must not provision a cache informer.
	APIReader       client.Reader
	Scheme          *runtime.Scheme
	RestConfig      *rest.Config
	ResourceManager *fluxssa.ResourceManager
	EventRecorder   events.EventRecorder
	Renderer        render.ModuleRenderer
	// DefaultServiceAccount is the fallback SA name used when a
	// ModuleInstance has an empty spec.serviceAccountName. Resolved in the
	// instance's own namespace. Empty disables the default.
	DefaultServiceAccount string
	// Kernel is the shared, long-lived library Kernel constructed once at
	// manager startup. It is the injection seam later enhancement-0001 slices
	// consume to drive the render path; this slice wires it but does not read
	// it on any reconcile path.
	Kernel *kernel.Kernel

	// MaxConcurrentRenders is the manager's --max-concurrent-renders, applied
	// as this controller's MaxConcurrentReconciles: how many ModuleInstances
	// reconcile at once. Renders are bounded separately, across both kinds,
	// by RenderSlots. Zero or negative keeps controller-runtime's default of
	// one.
	MaxConcurrentRenders int

	// RenderSlots is the process-wide render pool built from
	// --max-concurrent-renders and shared with the ModulePackage reconciler,
	// so the flag bounds renders of both kinds together. Nil leaves renders
	// unbounded (tests that need no bound).
	RenderSlots *render.Slots

	// warnings remembers each instance's last render warnings so RenderWarning
	// events are emitted on transition only (0019:D18).
	warnings opmreconcile.WarningTracker
}

// +kubebuilder:rbac:groups=opmodel.dev,resources=moduleinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opmodel.dev,resources=moduleinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opmodel.dev,resources=moduleinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=opmodel.dev,resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;impersonate
// +kubebuilder:rbac:groups="",resources=users;groups,verbs=impersonate
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update

// Reconcile runs the full ModuleInstance reconcile loop: CUE module synthesis
// and resolution from OCI registry, rendering, SSA apply, optional prune,
// and status commit.
func (r *ModuleInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling ModuleInstance", "name", req.Name, "namespace", req.Namespace)

	return opmreconcile.ReconcileModuleInstance(ctx, &opmreconcile.ModuleInstanceParams{
		Client:                r.Client,
		APIReader:             r.APIReader,
		RestConfig:            r.RestConfig,
		ResourceManager:       r.ResourceManager,
		EventRecorder:         r.EventRecorder,
		Renderer:              r.Renderer,
		RenderSlots:           r.RenderSlots,
		DefaultServiceAccount: r.DefaultServiceAccount,
		Warnings:              &r.warnings,
	}, req)
}

// SetupWithManager sets up the controller with the Manager.
//
// Watches:
//   - ModuleInstance CRs (primary, generation-change predicate)
//   - Platform (cluster singleton) — an update re-enqueues the operator-managed,
//     unsuspended ModuleInstances (moduleInstancePlatformIndex) via
//     mapPlatformToModuleInstances only when a field they consume moves
//     (platformConsumedFieldsChanged), so releases blocked on PlatformNotReady
//     recover promptly when the platform is generated, and instances rendered
//     under a superseded pin set or skew policy re-render, while a Platform
//     status write that only reports something renders nothing. The
//     generation predicate lives on For() (not as a global event filter) so
//     it does not suppress the Platform watch, whose trigger (the
//     reconciler's status update) does not bump generation.
//
// MaxConcurrentRenders (the manager's --max-concurrent-renders) becomes the
// controller's MaxConcurrentReconciles, so phases outside the render (apply,
// prune, deletion, suspend) never queue behind the other kind's renders. The
// renders themselves are bounded by RenderSlots, one pool shared with the
// ModulePackage controller: renders share nothing (library ADR-005, ADR-007),
// so the only bound is memory, and it holds across both kinds.
func (r *ModuleInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&releasesv1alpha1.ModuleInstance{}, moduleInstancePlatformIndex, moduleInstancePlatform); err != nil {
		return fmt.Errorf("indexing ModuleInstances by the Platform they render against: %w", err)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.ModuleInstance{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&releasesv1alpha1.Platform{},
			handler.EnqueueRequestsFromMapFunc(r.mapPlatformToModuleInstances),
			builder.WithPredicates(platformConsumedFieldsChanged()),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentRenders,
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](1*time.Second, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[ctrl.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		}).
		Named("moduleinstance").
		Complete(r)
}

// platformConsumedFieldsChanged passes a Platform update only when a field a
// ModuleInstance render consumes differs between the old and the new object:
//   - the Ready condition's status (the only True reason is Generated, so the
//     status alone carries the recovery edge),
//   - the pin set: status.packageIdentity, the field that identifies the pin
//     set an instance renders against, and status.registry beside it, so a
//     regression in how the identity is computed cannot silently stop a
//     re-render under a new pin,
//   - spec.skewPolicy,
//   - status.observedGeneration: the platform reconciler writes it with the
//     package it generated for that generation, so the event finds the new
//     package already in the store.
//
// metadata.generation is excluded: a spec edit bumps it before the platform
// is regenerated, so a render on that edge would run against the old
// package and the observedGeneration write that follows renders again.
//
// It also passes a status.operatorVersion change. The platform store is in
// memory, so after an operator upgrade every instance renders into
// PlatformNotReady before the platform is regenerated, and the status write
// that follows the regeneration differs only in operatorVersion: that event
// is what recovers them promptly.
//
// The Ready reason and message are excluded: a build failure rewrites the
// message per error and can move between False reasons, while a refusal
// keeps the last good package in the store, so neither changes what an
// instance renders against. ContractsFulfilled is a report too. Create,
// delete and generic events pass, and so does an update whose objects are
// not Platforms: failing open costs a render per instance, failing closed
// could leave one blocked.
func platformConsumedFieldsChanged() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			old, okOld := e.ObjectOld.(*releasesv1alpha1.Platform)
			cur, okNew := e.ObjectNew.(*releasesv1alpha1.Platform)
			if !okOld || !okNew {
				return true
			}
			return old.Status.ObservedGeneration != cur.Status.ObservedGeneration ||
				old.Status.PackageIdentity != cur.Status.PackageIdentity ||
				!equality.Semantic.DeepEqual(old.Status.Registry, cur.Status.Registry) ||
				skewPolicyOf(old) != skewPolicyOf(cur) ||
				readyStatus(old) != readyStatus(cur) ||
				old.Status.OperatorVersion != cur.Status.OperatorVersion
		},
	}
}

// readyStatus is the Platform's Ready condition status; absent reads as "".
func readyStatus(p *releasesv1alpha1.Platform) metav1.ConditionStatus {
	if c := apimeta.FindStatusCondition(p.Status.Conditions, status.ReadyCondition); c != nil {
		return c.Status
	}
	return ""
}

// skewPolicyOf is spec.skewPolicy as written; unset reads as "".
func skewPolicyOf(p *releasesv1alpha1.Platform) string {
	if p.Spec.SkewPolicy == nil {
		return ""
	}
	return *p.Spec.SkewPolicy
}

// moduleInstancePlatformIndex is the field index naming the Platform a
// ModuleInstance renders against.
const moduleInstancePlatformIndex = ".platform"

// moduleInstancePlatform is the Platform an instance renders against: the
// cluster singleton for an operator-managed, unsuspended instance, and none
// for a CLI-owned or suspended one, which the reconcile returns from before
// rendering. It mirrors those two early returns exactly.
func moduleInstancePlatform(obj client.Object) []string {
	mi, ok := obj.(*releasesv1alpha1.ModuleInstance)
	if !ok || mi.Spec.Owner == releasesv1alpha1.OwnerCLI || mi.Spec.Suspend {
		return nil
	}
	return []string{platformSingletonName}
}

// mapPlatformToModuleInstances enqueues the ModuleInstances that render
// against the changed Platform, through moduleInstancePlatformIndex. This
// unblocks releases sitting in PlatformNotReady the moment the platform is
// generated, rather than waiting for the transient backoff. CLI-owned and
// suspended instances are not enqueued: neither renders, and resuming one or
// handing it to the operator is a spec change that reconciles it through its
// own watch. A Platform not named for the singleton maps to nothing.
func (r *ModuleInstanceReconciler) mapPlatformToModuleInstances(ctx context.Context, obj client.Object) []reconcile.Request {
	var list releasesv1alpha1.ModuleInstanceList
	if err := r.List(ctx, &list, client.MatchingFields{moduleInstancePlatformIndex: obj.GetName()}); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list ModuleInstances for Platform-triggered re-enqueue")
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		mi := &list.Items[i]
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: mi.Name, Namespace: mi.Namespace},
		})
	}
	return reqs
}
