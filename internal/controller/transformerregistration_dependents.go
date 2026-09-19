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
	"slices"
	"strings"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// ClaimFinalizerName is the finalizer that holds a TransformerRegistration
// while instances still demand contracts it provides (0015:D3).
// It is distinct from the ModuleInstance cleanup finalizer: this one deletes
// nothing on release, it only decides when release is allowed.
const ClaimFinalizerName = "opmodel.dev/dependents"

// dependents returns the names of the ModuleInstances demanding at least one
// contract the claim provides, in name order.
//
// The demand is read from each instance's status.requiredContracts, which the
// instance's own reconcile derives from its rendered components. Nothing is
// re-rendered here, and nothing is acquired from a registry: the count must
// be answerable while the registry is unreachable, because a delete that can
// fail for reasons unconnected to the delete is worse than no guard at all
// (see the change's design.md for the measurement behind that).
//
// A claim providing nothing has no dependents, whatever any instance
// declares: the intersection is empty by construction, so the empty-provides
// case needs no special branch.
func (r *TransformerRegistrationReconciler) dependents(
	ctx context.Context,
	claim *releasesv1alpha1.TransformerRegistration,
) ([]string, error) {
	var list releasesv1alpha1.ModuleInstanceList
	if err := r.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("listing module instances: %w", err)
	}

	provided := make(map[string]struct{}, len(claim.Spec.Provides))
	for _, fqn := range claim.Spec.Provides {
		provided[fqn] = struct{}{}
	}

	var names []string
	for i := range list.Items {
		instance := &list.Items[i]
		for _, fqn := range instance.Status.RequiredContracts {
			if _, ok := provided[fqn]; ok {
				names = append(names, instance.Namespace+"/"+instance.Name)
				break
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

// reconcileDeletion runs when the claim carries a deletion timestamp: it
// blocks while dependents remain and releases the finalizer once they are
// gone.
//
// A claim without the finalizer is already released and is left entirely
// alone — including one that never carried it, which is every claim that was
// never accepted.
//
// The block is reported as Ready=False with DependentsRemain and leaves
// status.accepted and status.active untouched. A blocked claim has not failed
// acceptance and has not stopped serving; it is still the provider its
// dependents are rendering against, which is exactly why the block holds.
func (r *TransformerRegistrationReconciler) reconcileDeletion(
	ctx context.Context,
	claim *releasesv1alpha1.TransformerRegistration,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(claim, ClaimFinalizerName) {
		return ctrl.Result{}, nil
	}

	names, err := r.dependents(ctx, claim)
	if err != nil {
		// The dependents could not be listed, so the claim cannot be
		// released. Retry rather than release: releasing on an unreadable
		// answer is the abandonment the block exists to prevent.
		return ctrl.Result{}, fmt.Errorf("counting dependents of claim %s: %w", claim.Name, err)
	}

	if len(names) > 0 {
		patcher := patch.NewSerialPatcher(claim, r.Client)
		msg := fmt.Sprintf(
			"Deletion blocked: %d module instance(s) still demand contracts this claim provides (%s); "+
				"remove them before removing the provider",
			len(names), strings.Join(names, ", "))
		if r.transitioned(claim, status.DependentsRemainReason, msg) {
			r.EventRecorder.Eventf(claim, nil, corev1.EventTypeWarning,
				status.DependentsRemainReason, "Delete", "%s", msg)
		}
		status.MarkStalled(claim, status.DependentsRemainReason, "%s", msg)
		log.Info("Deletion blocked by dependents", "name", claim.Name, "dependents", len(names))

		// The interval requeue is the safety net, not the mechanism: the
		// ModuleInstance watch re-enqueues this claim when a dependent's
		// demand changes or the instance goes away, so the usual release is
		// prompt.
		return ctrl.Result{RequeueAfter: opmreconcile.StalledRecheckInterval},
			r.patchStatus(ctx, patcher, claim)
	}

	log.Info("No dependents remain, releasing claim for deletion", "name", claim.Name)
	if err := r.releaseClaim(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer from claim %s: %w", claim.Name, err)
	}
	return ctrl.Result{}, nil
}

// holdClaim marks the accepted claim as held, in memory. The caller's patcher
// commits it: the flux helper diffs metadata as well as status and sends both,
// so the finalizer and the verdict reach the API server through one patcher
// rather than through a metadata patch that would leave the patcher's base
// object a resourceVersion behind and every status patch after it in conflict.
//
// A finalizer patch does not bump metadata.generation, so it cannot wake this
// controller's own generation-filtered watch — which is why that watch also
// passes a deletion-timestamp change (claimSpecOrDeletionChanged).
func holdClaim(claim *releasesv1alpha1.TransformerRegistration) {
	controllerutil.AddFinalizer(claim, ClaimFinalizerName)
}

// releaseClaim removes the guard finalizer, letting the API server complete
// the deletion.
func (r *TransformerRegistrationReconciler) releaseClaim(
	ctx context.Context,
	claim *releasesv1alpha1.TransformerRegistration,
) error {
	mergePatch := client.MergeFrom(claim.DeepCopy())
	controllerutil.RemoveFinalizer(claim, ClaimFinalizerName)
	return r.Patch(ctx, claim, mergePatch)
}

// claimContributesAfterDeletion reports whether a claim carrying a deletion
// timestamp still belongs in the platform's active set: it does exactly while
// the guard finalizer holds it.
//
// This is the pairing the finalizer needs to mean anything. Without it the
// active set drops a claim the instant it is marked for deletion, so the
// catalog leaves the next generated platform while the block is still
// reported as holding — and the dependents the block is protecting lose their
// provider anyway, through the door the block was supposed to close.
func claimContributesAfterDeletion(claim *releasesv1alpha1.TransformerRegistration) bool {
	return controllerutil.ContainsFinalizer(claim, ClaimFinalizerName)
}
