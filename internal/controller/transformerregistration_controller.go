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
	"slices"
	"strings"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/open-platform-model/library/opm/catalog"
	oerrors "github.com/open-platform-model/library/opm/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

// CatalogAcquirer acquires a published catalog by coordinate. The manager
// passes the shared library Kernel, whose AcquireCatalogFromRegistry both
// fetches and shape-gates: an artifact of another kind is refused by the
// library, not by a rule this repo maintains (enhancement 0015 D10).
type CatalogAcquirer interface {
	AcquireCatalogFromRegistry(ctx context.Context, modPath, version string) (*catalog.Catalog, error)
}

// claimKind is the inventory Kind a rendered claim is recorded under.
const claimKind = "TransformerRegistration"

// claimGroup is the inventory Group a rendered claim is recorded under.
const claimGroup = "opmodel.dev"

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

	// Catalogs acquires the catalog a claim names. The manager passes the
	// shared, long-lived library Kernel constructed once at startup.
	Catalogs CatalogAcquirer

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

	// Lease, not Generated: the build-compatibility check below reads the
	// generated module directory off disk, and the PlatformReconciler's prune
	// keeps only the current generation plus every leased one. Without a
	// lease a regeneration landing mid-reconcile deletes the directory under
	// the read.
	generated, release, ok := r.Store.Lease()
	if !ok {
		// No platform to judge against. Not a refusal: the platform's absence
		// says nothing about the claim, and writing a verdict here would make
		// acceptance depend on which reconciler ran first. The Platform watch
		// re-enqueues the moment one is generated; the interval requeue is the
		// safety net.
		return r.deferVerdict(ctx, patcher, &claim,
			status.PlatformNotReadyReason,
			"No platform has been generated yet, so the claim cannot be judged")
	}
	defer release()

	// Acceptance re-derives every fact it judges: nothing on the claim is
	// trusted (enhancement 0015 D11). The catalog is the first operand, so it
	// is acquired before anything is compared.
	cat, err := r.Catalogs.AcquireCatalogFromRegistry(ctx, claim.Spec.Catalog, claim.Spec.Version)
	if err != nil {
		if errors.Is(err, oerrors.ErrWrongKind) {
			// The artifact resolved and is not a catalog. An authoring
			// problem: the claimant named the wrong module path. The
			// library's message carries the kind it found.
			return r.refuse(ctx, patcher, &claim, status.CatalogWrongKindReason,
				fmt.Sprintf("Claimed artifact %s at %s is not a catalog: %v",
					claim.Spec.Catalog, claim.Spec.Version, err))
		}
		// Nothing resolved. A registry or coordinate problem, which a
		// claimant fixes somewhere else entirely, so it gets its own reason.
		return r.refuse(ctx, patcher, &claim, status.CatalogUnresolvedReason,
			fmt.Sprintf("Claimed catalog %s at %s could not be resolved: %v",
				claim.Spec.Catalog, claim.Spec.Version, err))
	}

	if msg, ok := providesDrift(cat, claim.Spec.Provides); !ok {
		return r.refuse(ctx, patcher, &claim, status.ProvidesMismatchReason, msg)
	}

	switch verdict, msg := r.checkProviderIdentity(ctx, &claim); verdict {
	case identityPending:
		return r.deferVerdict(ctx, patcher, &claim, status.ProviderInventoryPendingReason, msg)
	case identityRefused:
		return r.refuse(ctx, patcher, &claim, status.ProviderMismatchReason, msg)
	case identityOK:
	}

	// D8 compares the catalog's committed requirements against the platform's
	// resolved versions, here rather than at render: a render failure would
	// name whichever unrelated module instance triggered the build, while
	// this names the provider that is incompatible.
	platformReqs, err := platformRequirements(generated.Dir)
	if err != nil {
		return r.deferVerdict(ctx, patcher, &claim, status.PlatformNotReadyReason,
			fmt.Sprintf("The generated platform's resolution could not be read: %v", err))
	}
	switch msg, err := buildIncompatibility(cat, platformReqs); {
	case err != nil:
		return r.refuse(ctx, patcher, &claim, status.BuildIncompatibleReason,
			fmt.Sprintf("Claimed catalog's committed requirements could not be read: %v", err))
	case msg != "":
		return r.refuse(ctx, patcher, &claim, status.BuildIncompatibleReason, msg)
	}

	holder, err := r.holderOf(ctx, &claim)
	if err != nil {
		return r.deferVerdict(ctx, patcher, &claim, status.DuplicateClaimReason,
			fmt.Sprintf("Competing claims for catalog %s could not be listed: %v", claim.Spec.Catalog, err))
	}
	if holder != "" {
		return r.refuse(ctx, patcher, &claim, status.DuplicateClaimReason, fmt.Sprintf(
			"Claim %s already holds provider catalog %s; remove one of the two claims",
			holder, claim.Spec.Catalog))
	}

	return ctrl.Result{}, r.accept(ctx, patcher, &claim)
}

// holderOf returns the name of the claim that holds this claim's provider
// catalog, or the empty string when this claim is the holder. At most one
// claim for a provider is accepted (enhancement 0015 D12); two instances of
// one provider module produce two distinct CRs, and the loser is refused here
// naming the winner.
//
// The holder is the claim with the earliest creationTimestamp, ties broken by
// name. Both fields are immutable, so the decision does not move when either
// claim is re-reconciled and does not depend on which one the informer
// happened to deliver first — which a first-accepted-wins rule recorded in
// status would, handing acceptance to the other claim after a manager
// restart.
func (r *TransformerRegistrationReconciler) holderOf(
	ctx context.Context,
	claim *releasesv1alpha1.TransformerRegistration,
) (string, error) {
	var list releasesv1alpha1.TransformerRegistrationList
	if err := r.List(ctx, &list); err != nil {
		return "", err
	}

	holder := claim
	for i := range list.Items {
		other := &list.Items[i]
		if other.Name == claim.Name || other.Spec.Catalog != claim.Spec.Catalog {
			continue
		}
		// A claim on its way out is not competing for the provider.
		if !other.DeletionTimestamp.IsZero() {
			continue
		}
		if olderClaim(other, holder) {
			holder = other
		}
	}

	if holder.Name == claim.Name {
		return "", nil
	}
	return holder.Name, nil
}

// olderClaim reports whether a should hold the provider ahead of b: the
// earlier creationTimestamp, and on a tie the lower name.
func olderClaim(a, b *releasesv1alpha1.TransformerRegistration) bool {
	if a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.Name < b.Name
	}
	return a.CreationTimestamp.Before(&b.CreationTimestamp)
}

// providesDrift compares the contract set re-derived from the catalog against
// the set the claim lists, for exact equality in both directions. A subset is
// not accepted: partial registration would leave the remainder reported as
// unfulfilled with no indication that the provider withheld it
// (enhancement 0015 D11). The message names both lists, because a claimant
// cannot act on "they differ".
//
// Both sides are sorted before comparison, so the verdict does not depend on
// the order either list was produced in.
func providesDrift(cat *catalog.Catalog, claimed []string) (string, bool) {
	derived, err := cat.Provides()
	if err != nil {
		return fmt.Sprintf("Claimed catalog's provider contracts could not be derived: %v", err), false
	}

	want := slices.Clone(claimed)
	slices.Sort(want)
	got := slices.Clone(derived)
	slices.Sort(got)

	if slices.Equal(want, got) {
		return "", true
	}
	return fmt.Sprintf(
		"Claim lists provider contracts [%s] but the catalog implements [%s]; acceptance requires them to match exactly",
		strings.Join(want, ", "), strings.Join(got, ", ")), false
}

// identityVerdict is the outcome of the provider-identity check.
type identityVerdict int

const (
	identityOK identityVerdict = iota
	identityPending
	identityRefused
)

// checkProviderIdentity verifies the claim came from the ModuleInstance its
// providerRef names, by looking the claim up in that instance's inventory.
//
// The inventory rather than the claim's labels: measured, a rendered claim
// carries an instance NAME label and no namespace or uuid label, so a
// label-only check would accept a stray claim from any instance sharing the
// provider's name in another namespace — the exact spoof the check exists to
// stop. CONSTITUTION Principle III already makes the inventory this repo's
// ownership record, so this reuses that answer rather than inventing a weaker
// second one. See the change's design.md for the measurement.
//
// An instance whose inventory has not been written yet is pending, not
// refused: the claim can reach the API server before its owner's status does,
// and a race is not a verdict.
func (r *TransformerRegistrationReconciler) checkProviderIdentity(
	ctx context.Context,
	claim *releasesv1alpha1.TransformerRegistration,
) (identityVerdict, string) {
	ref := claim.Spec.ProviderRef

	var instance releasesv1alpha1.ModuleInstance
	key := types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
	if err := r.Get(ctx, key, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return identityRefused, fmt.Sprintf(
				"Claim names provider ModuleInstance %s/%s, which does not exist, so the claim is not rendered output",
				ref.Namespace, ref.Name)
		}
		return identityPending, fmt.Sprintf(
			"Provider ModuleInstance %s/%s could not be read: %v", ref.Namespace, ref.Name, err)
	}

	if instance.Status.Inventory == nil {
		return identityPending, fmt.Sprintf(
			"Provider ModuleInstance %s/%s has not written an inventory yet", ref.Namespace, ref.Name)
	}

	for _, entry := range instance.Status.Inventory.Entries {
		if entry.Group == claimGroup && entry.Kind == claimKind && entry.Name == claim.Name {
			return identityOK, ""
		}
	}

	return identityRefused, fmt.Sprintf(
		"Claim %s names provider ModuleInstance %s/%s, whose inventory does not own it, so the claim did not come from the instance it names",
		claim.Name, ref.Namespace, ref.Name)
}

// accept records the claim as accepted. It does not activate: status.active
// stays false until an accepted claim's provider is reported serving, which
// is a later change.
//
// No requeue: an accepted claim is re-judged when its own spec changes (the
// generation predicate) or when the platform it was judged against is
// regenerated (the Platform watch), and nothing else can invalidate the
// verdict. A newly created competitor cannot take the provider from it,
// because a claim created later cannot carry an earlier creationTimestamp.
func (r *TransformerRegistrationReconciler) accept(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	claim *releasesv1alpha1.TransformerRegistration,
) error {
	claim.Status.ObservedGeneration = claim.Generation
	claim.Status.Accepted = true
	status.MarkReadyWithReason(claim, status.AcceptedReason,
		"Claim accepted for catalog %s at %s", claim.Spec.Catalog, claim.Spec.Version)
	return r.patchStatus(ctx, patcher, claim)
}

// refuse records a refusal naming what failed and the value that failed it.
// Acceptance is whole or it is a refusal, so status.accepted is set false
// rather than left at whatever a previous generation reached.
//
// The recheck is periodic rather than terminal: every refusal here resolves
// against mutable external state (a registry, another claim, the platform's
// resolution), so none of them is permanent. The warning event fires only on
// transition, so a recheck of an unchanged refusal does not spam events.
func (r *TransformerRegistrationReconciler) refuse(
	ctx context.Context,
	patcher *patch.SerialPatcher,
	claim *releasesv1alpha1.TransformerRegistration,
	reason, msg string,
) (ctrl.Result, error) {
	if r.transitioned(claim, reason, msg) {
		r.EventRecorder.Eventf(claim, nil, corev1.EventTypeWarning, reason, "Accept", "%s", msg)
	}
	claim.Status.ObservedGeneration = claim.Generation
	claim.Status.Accepted = false
	status.MarkStalled(claim, reason, "%s", msg)
	return ctrl.Result{RequeueAfter: opmreconcile.StalledRecheckInterval}, r.patchStatus(ctx, patcher, claim)
}

// transitioned reports whether the claim is newly entering this refusal, or
// entering it with a different reason or message than it already carries.
func (r *TransformerRegistrationReconciler) transitioned(
	claim *releasesv1alpha1.TransformerRegistration,
	reason, msg string,
) bool {
	prior := apimeta.FindStatusCondition(claim.Status.Conditions, status.ReadyCondition)
	return prior == nil ||
		prior.Status != metav1.ConditionFalse ||
		prior.Reason != reason ||
		prior.Message != msg
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
