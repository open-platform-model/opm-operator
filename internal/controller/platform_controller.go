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

	"github.com/fluxcd/pkg/runtime/patch"
	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// platformSingletonName is the only permitted name for the cluster-scoped
// Platform singleton. The CRD enforces this via a CEL rule; the reconciler
// guards on it again as defense-in-depth (enhancement 0001 §8.1).
const platformSingletonName = "cluster"

// PlatformReconciler reconciles the singleton Platform CR into a materialized
// platform held in a process-local, generation-keyed store. On reconcile it
// maps the spec to a synth.PlatformInput, runs SynthesizePlatform then
// Materialize, holds the result, and surfaces the outcome on the CR's Ready
// condition. Nothing yet reads the store; the CR's status is the observable
// contract this slice delivers.
type PlatformReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder events.EventRecorder

	// Kernel is the shared, long-lived library Kernel constructed once at
	// manager startup. SynthesizePlatform and Materialize run on it.
	Kernel *kernel.Kernel

	// Store holds the current materialized platform. Written here, read by
	// the render path in a later slice.
	Store *platformstore.Store
}

// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups=releases.opmodel.dev,resources=platforms/status,verbs=get;update;patch

// Reconcile materializes the cluster-singleton Platform and records the
// outcome on its status. It reconciles only the object named "cluster";
// any other name is ignored without error. On delete it clears the store
// slot (workloads are untouched — §8.4 freeze-don't-teardown).
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
			log.Info("Platform deleted, cleared materialized-platform store", "name", req.Name)
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

	in := platformInput(&plat)

	// SchemaCache is left nil; SynthesizePlatform defaults it to the Kernel's
	// cache, preserving the one-Cache-per-process invariant.
	p, err := r.Kernel.SynthesizePlatform(ctx, in)
	if err != nil {
		// Synthesis failures (schema/registry access, malformed spec) are not
		// MaterializeErrors. Surface as stalled and do not requeue endlessly;
		// a spec change re-triggers via the generation predicate.
		status.MarkStalled(&plat, status.MaterializeFailedReason, "synthesizing platform: %v", err)
		r.EventRecorder.Eventf(&plat, nil, corev1.EventTypeWarning, status.MaterializeFailedReason, "Materialize", "Synthesizing platform: %v", err)
		return ctrl.Result{}, r.patchStatus(ctx, patcher, &plat)
	}

	mp, err := r.Kernel.Materialize(ctx, p)
	if err != nil {
		if me, ok := errors.AsType[*oerrors.MaterializeError](err); ok {
			msg := fmt.Sprintf("materialize failed: kind=%s subscription=%q version=%q: %v",
				me.Kind, me.Subscription, me.Version, me.Cause)
			status.MarkStalled(&plat, status.MaterializeFailedReason, "%s", msg)
			r.EventRecorder.Eventf(&plat, nil, corev1.EventTypeWarning, status.MaterializeFailedReason, "Materialize", "%s", msg)
		} else {
			status.MarkStalled(&plat, status.MaterializeFailedReason, "materializing platform: %v", err)
			r.EventRecorder.Eventf(&plat, nil, corev1.EventTypeWarning, status.MaterializeFailedReason, "Materialize", "Materializing platform: %v", err)
		}
		// Do NOT touch the store: a transient failure must not blank the
		// platform the cluster is running on (§8.4 freeze posture).
		return ctrl.Result{}, r.patchStatus(ctx, patcher, &plat)
	}

	// Success: hold the materialized platform under the generation key and
	// mark Ready.
	r.Store.Set(plat.Generation, mp)
	plat.Status.ObservedGeneration = plat.Generation
	status.MarkReadyWithReason(&plat, status.MaterializedReason, "Platform materialized")
	r.EventRecorder.Eventf(&plat, nil, corev1.EventTypeNormal, status.MaterializedReason, "Materialize", "Platform materialized for generation %d", plat.Generation)

	log.Info("Platform materialized", "name", plat.Name, "generation", plat.Generation)
	return ctrl.Result{}, r.patchStatus(ctx, patcher, &plat)
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

// platformInput maps a Platform CR to the typed synth.PlatformInput. The CRD
// was authored as a 1:1 projection of the core #Platform surface, so the
// mapping is mechanical. SchemaCache is left nil for the Kernel to default.
func platformInput(plat *releasesv1alpha1.Platform) synth.PlatformInput {
	in := synth.PlatformInput{
		Name:        plat.Name,
		Type:        plat.Spec.Type,
		Labels:      plat.Labels,
		Annotations: plat.Annotations,
	}
	if len(plat.Spec.Registry) > 0 {
		in.Subscriptions = make(map[string]synth.SubscriptionSpec, len(plat.Spec.Registry))
		for path, sub := range plat.Spec.Registry {
			spec := synth.SubscriptionSpec{Enable: sub.Enable}
			if sub.Filter != nil {
				spec.Filter = &synth.FilterSpec{
					Range: sub.Filter.Range,
					Allow: sub.Filter.Allow,
					Deny:  sub.Filter.Deny,
				}
			}
			in.Subscriptions[path] = spec
		}
	}
	return in
}

// SetupWithManager wires the controller into mgr, watching the Platform
// singleton with a generation-change predicate.
func (r *PlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasesv1alpha1.Platform{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("platform").
		Complete(r)
}
