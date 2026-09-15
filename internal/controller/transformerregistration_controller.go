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

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/open-platform-model/library/opm/kernel"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// TransformerRegistrationReconciler judges a provider module's claim that its
// catalog implements platform contracts (enhancement 0015 D3). It decides one
// transition — whether a claim is accepted — and records the verdict on the
// claim's own status as conditions, accepted and observedGeneration.
//
// The kind gets a reconciler of its own rather than a branch of the Platform
// reconciler: acceptance is per-claim and its verdict lives on the claim, so
// folding it in would make one claim's failure a platform-level failure,
// which is the misattribution acceptance exists to avoid.
//
// It judges nothing until the platform exists. The built platform is read
// through the process-local store, never built here; a claim arriving before
// the Platform reconciler has generated one is requeued, because a verdict
// that depends on reconcile order is not a verdict.
//
// Acceptance does not activate: status.active stays false, and an accepted
// claim changes what the object reports, not what the cluster renders.
type TransformerRegistrationReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder events.EventRecorder

	// Kernel is the shared, long-lived library Kernel constructed once at
	// manager startup. Acquiring the claimed catalog runs on it.
	Kernel *kernel.Kernel

	// Store holds the platform the Platform reconciler generated and built.
	// Read-only here: acceptance judges against it and never writes it.
	Store *platformstore.Store
}

// +kubebuilder:rbac:groups=opmodel.dev,resources=transformerregistrations,verbs=get;list;watch
// +kubebuilder:rbac:groups=opmodel.dev,resources=transformerregistrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opmodel.dev,resources=moduleinstances,verbs=get;list;watch

// Reconcile records a verdict on one claim. A claim mid-deletion is left
// alone: the finalizer and the lifecycle edges belong to the activation
// change, so there is nothing to clean up here.
func (r *TransformerRegistrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var claim releasesv1alpha1.TransformerRegistration
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !claim.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling TransformerRegistration", "name", claim.Name, "generation", claim.Generation)

	// Snapshot before mutation so the serial patcher diffs against the
	// pre-reconcile status.
	patcher := patch.NewSerialPatcher(&claim, r.Client)

	if _, ok := r.Store.Generated(); !ok {
		// No platform to judge against. Not a refusal: the platform's absence
		// says nothing about the claim, and writing a verdict here would make
		// acceptance depend on which reconciler ran first. The Platform watch
		// re-enqueues the moment one is generated; the interval requeue is the
		// safety net.
		return r.deferVerdict(ctx, patcher, &claim,
			status.PlatformNotReadyReason,
			"No platform has been generated yet, so the claim cannot be judged")
	}

	// Every check is still to come. Until then a claim reconciles to the
	// first of the three states enhancement 0015 D3 defines.
	return r.deferVerdict(ctx, patcher, &claim,
		status.NotYetJudgedReason,
		"The claim is recorded and awaiting acceptance")
}

// deferVerdict records that no verdict has been reached for this generation
// and requeues. It writes Ready=Unknown rather than Ready=False: a refusal
// says the claim was judged and failed, which is not what happened, and
// status.accepted is left untouched so nothing downstream reads a verdict
// that was never taken.
func (r *TransformerRegistrationReconciler) deferVerdict(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	claim *releasesv1alpha1.TransformerRegistration,
	reason, msg string,
) (ctrl.Result, error) {
	claim.Status.ObservedGeneration = claim.Generation
	status.MarkReconciling(claim, reason, "%s", msg)
	return ctrl.Result{RequeueAfter: opmreconcile.StalledRecheckInterval}, r.patchStatus(ctx, patcher, claim)
}

// patchStatus commits the claim's status via the serial patcher, declaring the
// Ready/Reconciling/Stalled conditions this controller owns.
func (r *TransformerRegistrationReconciler) patchStatus(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	claim *releasesv1alpha1.TransformerRegistration,
) error {
	return patcher.Patch(ctx, claim,
		patch.WithOwnedConditions{
			Conditions: []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
			},
		},
	)
}

// SetupWithManager wires the controller into mgr. The generation predicate
// sits on For() rather than as a global filter so it does not suppress the
// Platform watch, whose trigger (the Platform reconciler's status update)
// does not bump a generation. That watch is what lets a claim parked on
// PlatformNotReady recover as soon as the platform is generated, instead of
// waiting out the interval requeue.
func (r *TransformerRegistrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.TransformerRegistration{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&releasesv1alpha1.Platform{},
			handler.EnqueueRequestsFromMapFunc(r.mapPlatformToRegistrations),
		).
		Named("transformerregistration").
		Complete(r)
}

// mapPlatformToRegistrations enqueues every claim in the cluster when the
// singleton Platform changes. List-all is cheap: the Platform is a cluster
// singleton, its changes are rare, and a claim is one per provider instance.
func (r *TransformerRegistrationReconciler) mapPlatformToRegistrations(ctx context.Context, _ client.Object) []reconcile.Request {
	var list releasesv1alpha1.TransformerRegistrationList
	if err := r.List(ctx, &list); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list TransformerRegistrations for Platform-triggered re-enqueue")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&list.Items[i]),
		})
	}
	return requests
}
