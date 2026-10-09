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

package reconcile_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// recordedEvent is one event as the reconciler emitted it, action included
// (the stock FakeRecorder drops the action).
type recordedEvent struct {
	eventType, reason, action, note string
}

// actionRecorder records every event with its action.
type actionRecorder struct {
	events []recordedEvent
}

func (r *actionRecorder) Eventf(_, _ runtime.Object, eventType, reason, action, note string, args ...any) {
	r.events = append(r.events, recordedEvent{eventType, reason, action, fmt.Sprintf(note, args...)})
}

func (r *actionRecorder) withReason(reason string) []recordedEvent {
	var out []recordedEvent
	for _, e := range r.events {
		if e.reason == reason {
			out = append(out, e)
		}
	}
	return out
}

// claimResources renders one PersistentVolumeClaim per name, owned by the
// stub instance like namedConfigMapRenderResult's ConfigMaps.
func claimResources(names ...string) []*object.Resource {
	out := make([]*object.Resource, 0, len(names))
	for _, name := range names {
		out = append(out, classedClaimResource(name, ""))
	}
	return out
}

// classedClaimResource renders one PersistentVolumeClaim. A storageClass that is
// not empty sets spec.storageClassName, which the API server refuses to
// change on a live claim.
func classedClaimResource(name, storageClass string) *object.Resource {
	class := ""
	if storageClass != "" {
		class = fmt.Sprintf("storageClassName: %q", storageClass)
	}
	v := cuecontext.New().CompileString(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "PersistentVolumeClaim"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
			%q: %q
		}
	}
	spec: {
		accessModes: ["ReadWriteOnce"]
		resources: requests: storage: "1Gi"
		%s
	}
}`, name, namespace,
		labels.ManagedBy, labels.ManagedByController,
		labels.ModuleInstanceNamespace, namespace,
		labels.ModuleInstanceUUID, stubInstanceUUID,
		class))
	if v.Err() != nil {
		panic(fmt.Sprintf("compiling stub PersistentVolumeClaim: %v", v.Err()))
	}
	return &object.Resource{Value: v, Instance: name, Component: name, Transformer: "kubernetes#simple"}
}

// renderWithClassedClaim is a render of the named ConfigMaps and one
// PersistentVolumeClaim of the given storage class.
func renderWithClassedClaim(configMaps []string, claim, storageClass string) *render.RenderResult {
	result := namedConfigMapRenderResult(configMaps...)
	result.Resources = append(result.Resources, classedClaimResource(claim, storageClass))
	return result
}

// settledClaim returns the claim as the cluster holds it, without its
// pvc-protection finalizer. envtest runs no controller that clears the
// finalizer, so without this a delete of the claim never completes and an
// apply that recreates the claim waits for ever.
func settledClaim(name string) *corev1.PersistentVolumeClaim {
	GinkgoHelper()
	var pvc corev1.PersistentVolumeClaim
	nn := types.NamespacedName{Name: name, Namespace: namespace}
	Expect(k8sClient.Get(ctx, nn, &pvc)).To(Succeed())
	pvc.Finalizers = nil
	Expect(k8sClient.Update(ctx, &pvc)).To(Succeed())
	Expect(k8sClient.Get(ctx, nn, &pvc)).To(Succeed())
	return &pvc
}

// expectSameClaim asserts that the claim is the object it was: same UID, same
// storage class, and no delete reached it.
func expectSameClaim(before *corev1.PersistentVolumeClaim) {
	GinkgoHelper()
	var after corev1.PersistentVolumeClaim
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: before.Name, Namespace: namespace}, &after)).To(Succeed())
	Expect(after.UID).To(Equal(before.UID), "claim %s must not be recreated", before.Name)
	Expect(after.DeletionTimestamp.IsZero()).To(BeTrue(), "no delete may reach claim %s", before.Name)
	Expect(after.Spec.StorageClassName).To(Equal(before.Spec.StorageClassName))
}

// expectClaimRecreated asserts that a new claim of the given storage class
// took the place of before.
func expectClaimRecreated(before *corev1.PersistentVolumeClaim, storageClass string) {
	GinkgoHelper()
	var after corev1.PersistentVolumeClaim
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: before.Name, Namespace: namespace}, &after)).To(Succeed())
	Expect(after.UID).NotTo(Equal(before.UID), "claim %s must be a new object", before.Name)
	Expect(after.Spec.StorageClassName).To(HaveValue(Equal(storageClass)))
}

// expectClaimConflict asserts the Ready condition and the one event of an
// apply that kept the claim.
func expectClaimConflict(conds []metav1.Condition, rec *actionRecorder, claim string) {
	GinkgoHelper()
	ready := apimeta.FindStatusCondition(conds, status.ReadyCondition)
	Expect(ready).NotTo(BeNil())
	Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	Expect(ready.Reason).To(Equal(status.ClaimConflictReason))
	Expect(ready.Message).To(ContainSubstring("PersistentVolumeClaim " + namespace + "/" + claim))
	Expect(ready.Message).To(ContainSubstring("refused the update of spec"))
	Expect(ready.Message).To(ContainSubstring("immutable"), "the API server's reason must be in the message")
	Expect(ready.Message).To(ContainSubstring("set spec.dataPolicy to Delete"))
	Expect(apimeta.FindStatusCondition(conds, status.StalledCondition)).To(BeNil(),
		"a claim conflict retries on the backoff and is not stalled")

	events := rec.withReason(status.ClaimConflictReason)
	Expect(events).To(HaveLen(1))
	Expect(events[0].eventType).To(Equal(corev1.EventTypeWarning))
	Expect(events[0].action).To(Equal("Apply"))
	Expect(events[0].note).To(Equal(ready.Message))
	Expect(rec.withReason(status.ApplyFailedReason)).To(BeEmpty(), "the conflict is reported once, under its own reason")
	Expect(rec.withReason(status.AppliedReason)).To(BeEmpty())
}

// renderOf is a render of the named ConfigMaps and PersistentVolumeClaims.
func renderOf(configMaps, claims []string) *render.RenderResult {
	result := namedConfigMapRenderResult(configMaps...)
	result.Resources = append(result.Resources, claimResources(claims...)...)
	return result
}

// claimState reads a claim: whether it exists, and whether a delete reached
// it. envtest runs no controller that clears the pvc-protection finalizer, so
// a deleted claim stays, with a deletion timestamp.
func claimState(name string) (exists, deleteRequested bool) {
	GinkgoHelper()
	var pvc corev1.PersistentVolumeClaim
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &pvc)
	if apierrors.IsNotFound(err) {
		return false, true
	}
	Expect(err).NotTo(HaveOccurred())
	return true, !pvc.DeletionTimestamp.IsZero()
}

func expectClaimUntouched(name string) {
	GinkgoHelper()
	exists, deleteRequested := claimState(name)
	Expect(exists).To(BeTrue(), "claim %s must exist", name)
	Expect(deleteRequested).To(BeFalse(), "claim %s must not be deleted", name)
}

func expectClaimDeleted(name string) {
	GinkgoHelper()
	_, deleteRequested := claimState(name)
	Expect(deleteRequested).To(BeTrue(), "claim %s must be deleted", name)
}

// removeClaims force-removes claims after a test.
func removeClaims(names ...string) {
	GinkgoHelper()
	for _, name := range names {
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &pvc); err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			continue
		}
		pvc.Finalizers = nil
		Expect(k8sClient.Update(ctx, &pvc)).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &pvc))).To(Succeed())
	}
}

func removeConfigMaps(names ...string) {
	GinkgoHelper()
	for _, name := range names {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		}))).To(Succeed())
	}
}

func configMapExists(name string) bool {
	GinkgoHelper()
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.ConfigMap{})
	if apierrors.IsNotFound(err) {
		return false
	}
	Expect(err).NotTo(HaveOccurred())
	return true
}

// createClaimInstance creates a ModuleInstance with the given prune and data
// policy and registers its finalizer.
func createClaimInstance(
	name string, prune bool, policy releasesv1alpha1.DataPolicy, params *opmreconcile.ModuleInstanceParams,
) types.NamespacedName {
	GinkgoHelper()
	mi := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module:     releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
			Prune:      prune,
			DataPolicy: policy,
		},
	}
	Expect(k8sClient.Create(ctx, mi)).To(Succeed())
	nn := types.NamespacedName{Name: name, Namespace: namespace}
	ensureFinalizer(params, nn)
	return nn
}

func reconcileInstance(params *opmreconcile.ModuleInstanceParams, nn types.NamespacedName) error {
	_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
	return err
}

func inventoryNames(inv *releasesv1alpha1.Inventory) []string {
	if inv == nil {
		return nil
	}
	names := make([]string, 0, len(inv.Entries))
	for _, e := range inv.Entries {
		names = append(names, e.Kind+"/"+e.Name)
	}
	return names
}

// expectConfigMapGone waits until the ConfigMap is gone: a delete with
// Foreground propagation ends when the collector helper has run.
func expectConfigMapGone(name string, description ...any) {
	GinkgoHelper()
	expectGone(types.NamespacedName{Name: name, Namespace: namespace}, &corev1.ConfigMap{}, description...)
}

func expectInstanceGone(nn types.NamespacedName) {
	GinkgoHelper()
	err := k8sClient.Get(ctx, nn, &releasesv1alpha1.ModuleInstance{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the ModuleInstance must be gone, got %v", err)
}

var _ = Describe("PersistentVolumeClaims of a ModuleInstance", func() {
	var (
		rec    *actionRecorder
		params *opmreconcile.ModuleInstanceParams
	)

	BeforeEach(func() {
		rec = &actionRecorder{}
		params = reconcileParams()
		params.EventRecorder = rec
	})

	It("keeps a stale claim by default, drops it from the inventory and prunes every other kind", func() {
		nn := createClaimInstance("claims-prune-keep", true, "", params)
		DeferCleanup(func() {
			removeClaims("cpk-data")
			removeConfigMaps("cpk-a", "cpk-b")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderOf([]string{"cpk-a", "cpk-b"}, []string{"cpk-data"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(inventoryNames(mi.Status.Inventory)).To(ConsistOf(
			"ConfigMap/cpk-a", "ConfigMap/cpk-b", "PersistentVolumeClaim/cpk-data"))

		By("the render drops the claim and one ConfigMap")
		params.Renderer = &stubRenderer{result: renderOf([]string{"cpk-a"}, nil)}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectClaimUntouched("cpk-data")
		expectConfigMapGone("cpk-b", "a stale ConfigMap is pruned as before")
		Expect(configMapExists("cpk-a")).To(BeTrue())

		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(inventoryNames(mi.Status.Inventory)).To(ConsistOf("ConfigMap/cpk-a"))
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal(status.ReconciliationSucceededReason))
		Expect(mi.Status.FailureCounters == nil || mi.Status.FailureCounters.Prune == 0).To(BeTrue())

		kept := rec.withReason(status.ClaimsKeptReason)
		Expect(kept).To(HaveLen(1))
		Expect(kept[0].eventType).To(Equal(corev1.EventTypeNormal))
		Expect(kept[0].action).To(Equal("Prune"))
		Expect(kept[0].note).To(ContainSubstring("Kept 1 PersistentVolumeClaim(s)"))
		Expect(kept[0].note).To(ContainSubstring(namespace + "/cpk-data"))
		Expect(rec.withReason(status.PrunedReason)).To(HaveLen(1))

		By("the next reconcile with unchanged inputs is a no-op and reports nothing again")
		Expect(reconcileInstance(params, nn)).To(Succeed())
		Expect(rec.withReason(status.NoOpReason)).To(HaveLen(1))
		Expect(rec.withReason(status.ClaimsKeptReason)).To(HaveLen(1))
		expectClaimUntouched("cpk-data")

		By("a later Delete policy does not reach the claim kept earlier")
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		mi.Spec.DataPolicy = releasesv1alpha1.DataPolicyDelete
		Expect(k8sClient.Update(ctx, &mi)).To(Succeed())
		Expect(reconcileInstance(params, nn)).To(Succeed())
		expectClaimUntouched("cpk-data")
	})

	It("keeps a stale claim when only the claim is stale, without a Pruned event", func() {
		nn := createClaimInstance("claims-prune-only", true, releasesv1alpha1.DataPolicyKeep, params)
		DeferCleanup(func() {
			removeClaims("cpo-data")
			removeConfigMaps("cpo-a")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderOf([]string{"cpo-a"}, []string{"cpo-data"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		params.Renderer = &stubRenderer{result: renderOf([]string{"cpo-a"}, nil)}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectClaimUntouched("cpo-data")
		Expect(rec.withReason(status.ClaimsKeptReason)).To(HaveLen(1))
		Expect(rec.withReason(status.PrunedReason)).To(BeEmpty())
	})

	It("prunes a stale claim when spec.dataPolicy is Delete", func() {
		nn := createClaimInstance("claims-prune-delete", true, releasesv1alpha1.DataPolicyDelete, params)
		DeferCleanup(func() {
			removeClaims("cpd-data")
			removeConfigMaps("cpd-a")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderOf([]string{"cpd-a"}, []string{"cpd-data"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		params.Renderer = &stubRenderer{result: renderOf([]string{"cpd-a"}, nil)}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectClaimDeleted("cpd-data")
		Expect(rec.withReason(status.ClaimsKeptReason)).To(BeEmpty())
		Expect(rec.withReason(status.PrunedReason)).To(HaveLen(1))
	})

	It("deletes nothing when spec.dataPolicy is Delete and spec.prune is not set", func() {
		nn := createClaimInstance("claims-noprune", false, releasesv1alpha1.DataPolicyDelete, params)
		DeferCleanup(func() {
			removeClaims("cnp-data")
			removeConfigMaps("cnp-a", "cnp-b")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderOf([]string{"cnp-a", "cnp-b"}, []string{"cnp-data"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		params.Renderer = &stubRenderer{result: renderOf([]string{"cnp-a"}, nil)}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectClaimUntouched("cnp-data")
		Expect(configMapExists("cnp-b")).To(BeTrue())
		Expect(rec.withReason(status.ClaimsKeptReason)).To(BeEmpty(), "nothing was up for deletion")
	})

	It("takes a kept claim back when a later render holds it again", func() {
		nn := createClaimInstance("claims-takeback", true, "", params)
		DeferCleanup(func() {
			removeClaims("ctb-data")
			removeConfigMaps("ctb-a")
			cleanupInstance(nn)
		})

		with := &stubRenderer{result: renderOf([]string{"ctb-a"}, []string{"ctb-data"})}
		without := &stubRenderer{result: renderOf([]string{"ctb-a"}, nil)}
		params.Renderer = with
		Expect(reconcileInstance(params, nn)).To(Succeed())
		params.Renderer = without
		Expect(reconcileInstance(params, nn)).To(Succeed())
		expectClaimUntouched("ctb-data")

		params.Renderer = with
		Expect(reconcileInstance(params, nn)).To(Succeed())

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(inventoryNames(mi.Status.Inventory)).To(ConsistOf("ConfigMap/ctb-a", "PersistentVolumeClaim/ctb-data"))
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		expectClaimUntouched("ctb-data")
	})

	// The record an operator without the field, or a CLI prune, leaves
	// behind: an inventory that lists a claim. The object has no
	// spec.dataPolicy, so the claim is protected from the first reconcile.
	It("protects a claim recorded before the field existed and after a handover from the CLI", func() {
		for _, tc := range []struct {
			name, claim, cm, managedBy string
			startOwner                 releasesv1alpha1.OwnerType
		}{
			{"claims-upgrade", "cup-data", "cup-a", labels.ManagedByController, ""},
			{"claims-handover", "cho-data", "cho-a", labels.ManagedByCLI, releasesv1alpha1.OwnerCLI},
		} {
			By(tc.name)
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: tc.claim, Namespace: namespace, Labels: map[string]string{
					labels.ManagedBy:          tc.managedBy,
					labels.ModuleInstanceUUID: stubInstanceUUID,
				}},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())

			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: namespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  tc.startOwner,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Prune:  true,
				},
			}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := types.NamespacedName{Name: tc.name, Namespace: namespace}
			DeferCleanup(func() {
				removeClaims(tc.claim)
				removeConfigMaps(tc.cm)
				cleanupInstance(nn)
			})

			mi.Status.InstanceUUID = stubInstanceUUID
			mi.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: 1, Count: 1,
				Entries: []releasesv1alpha1.InventoryEntry{{
					Kind: "PersistentVolumeClaim", Version: "v1", Namespace: namespace, Name: tc.claim,
				}},
			}
			Expect(k8sClient.Status().Update(ctx, mi)).To(Succeed())

			if tc.startOwner == releasesv1alpha1.OwnerCLI {
				Expect(k8sClient.Get(ctx, nn, mi)).To(Succeed())
				mi.Spec.Owner = releasesv1alpha1.OwnerOperator
				Expect(k8sClient.Update(ctx, mi)).To(Succeed())
			}

			rec.events = nil
			params.Renderer = &stubRenderer{result: renderOf([]string{tc.cm}, nil)}
			ensureFinalizer(params, nn)
			Expect(reconcileInstance(params, nn)).To(Succeed())

			expectClaimUntouched(tc.claim)
			var got releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			Expect(inventoryNames(got.Status.Inventory)).To(ConsistOf("ConfigMap/" + tc.cm))
			Expect(rec.withReason(status.ClaimsKeptReason)).To(HaveLen(1))
		}
	})

	It("keeps claims when the instance is deleted, and the delete completes", func() {
		nn := createClaimInstance("claims-delete-keep", true, "", params)
		DeferCleanup(func() {
			removeClaims("cdk-config", "cdk-cache")
			removeConfigMaps("cdk-a")
		})

		params.Renderer = &stubRenderer{result: renderOf([]string{"cdk-a"}, []string{"cdk-config", "cdk-cache"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &mi)).To(Succeed())
		rec.events = nil
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectInstanceGone(nn)
		expectConfigMapGone("cdk-a")
		expectClaimUntouched("cdk-config")
		expectClaimUntouched("cdk-cache")

		kept := rec.withReason(status.ClaimsKeptReason)
		Expect(kept).To(HaveLen(1))
		Expect(kept[0].eventType).To(Equal(corev1.EventTypeNormal))
		Expect(kept[0].action).To(Equal("Delete"))
		Expect(kept[0].note).To(ContainSubstring("Kept 2 PersistentVolumeClaim(s)"))
		Expect(kept[0].note).To(ContainSubstring(namespace + "/cdk-config"))
		Expect(kept[0].note).To(ContainSubstring(namespace + "/cdk-cache"))
	})

	It("completes the delete of an instance whose inventory holds only claims", func() {
		nn := createClaimInstance("claims-delete-only", true, "", params)
		DeferCleanup(func() { removeClaims("cdo-data") })

		params.Renderer = &stubRenderer{result: renderOf(nil, []string{"cdo-data"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &mi)).To(Succeed())
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectInstanceGone(nn)
		expectClaimUntouched("cdo-data")
	})

	It("deletes claims with the instance when spec.dataPolicy is Delete", func() {
		nn := createClaimInstance("claims-delete-delete", true, releasesv1alpha1.DataPolicyDelete, params)
		DeferCleanup(func() {
			removeClaims("cdd-data")
			removeConfigMaps("cdd-a")
		})

		params.Renderer = &stubRenderer{result: renderOf([]string{"cdd-a"}, []string{"cdd-data"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &mi)).To(Succeed())
		rec.events = nil
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectInstanceGone(nn)
		expectClaimDeleted("cdd-data")
		expectConfigMapGone("cdd-a")
		Expect(rec.withReason(status.ClaimsKeptReason)).To(BeEmpty())
	})

	It("still holds the finalizer when another delete fails, and keeps the claim", func() {
		nn := createClaimInstance("claims-delete-fail", true, "", params)
		DeferCleanup(func() {
			removeClaims("cdf-data")
			removeConfigMaps("cdf-a")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderOf([]string{"cdf-a"}, []string{"cdf-data"})}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &mi)).To(Succeed())

		realClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		params.Client = interceptor.NewClient(realClient, interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if obj.GetName() == "cdf-a" {
					return fmt.Errorf("injected delete failure")
				}
				return c.Delete(ctx, obj, opts...)
			},
		})
		rec.events = nil
		Expect(reconcileInstance(params, nn)).To(MatchError(ContainSubstring("injected delete failure")))

		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed(), "the finalizer must hold the instance")
		Expect(mi.Finalizers).To(ContainElement(opmreconcile.FinalizerName))
		expectClaimUntouched("cdf-data")
		Expect(rec.withReason(status.ClaimsKeptReason)).To(BeEmpty(), "the cleanup has not completed")
	})

	// forceInstance creates an instance with spec.rollout.forceConflicts and
	// without spec.prune: the forced recreate follows the policy alone.
	forceInstance := func(name string, policy releasesv1alpha1.DataPolicy) types.NamespacedName {
		GinkgoHelper()
		nn := createClaimInstance(name, false, policy, params)
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		mi.Spec.Rollout = &releasesv1alpha1.RolloutSpec{ForceConflicts: true}
		Expect(k8sClient.Update(ctx, &mi)).To(Succeed())
		return nn
	}

	It("keeps a claim with a changed immutable field under forceConflicts and reports a conflict", func() {
		nn := forceInstance("claims-force-keep", "")
		DeferCleanup(func() {
			removeClaims("cfk-data")
			removeConfigMaps("cfk-a", "cfk-b")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderWithClassedClaim([]string{"cfk-a"}, "cfk-data", "fast")}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		before := settledClaim("cfk-data")
		rec.events = nil

		By("the render changes the storage class of the claim and adds a ConfigMap")
		params.Renderer = &stubRenderer{result: renderWithClassedClaim([]string{"cfk-a", "cfk-b"}, "cfk-data", "slow")}
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred(), "a claim conflict is reported in the status, not as a reconcile error")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "the conflict is retried on the backoff")
		Expect(result.RequeueAfter).To(BeNumerically("<=", 5*time.Minute))

		expectSameClaim(before)
		expectConfigMapGone("cfk-b", "no object of the refused render may be applied")

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		expectClaimConflict(mi.Status.Conditions, rec, "cfk-data")
		Expect(inventoryNames(mi.Status.Inventory)).To(ConsistOf("ConfigMap/cfk-a", "PersistentVolumeClaim/cfk-data"),
			"the inventory stays that of the last applied render")
		Expect(mi.Status.FailureCounters).NotTo(BeNil())
		Expect(mi.Status.FailureCounters.Apply).To(BeNumerically(">", 0))

		By("the next reconcile decides the conflict again and still keeps the claim")
		Expect(reconcileInstance(params, nn)).To(Succeed())
		expectSameClaim(before)
		Expect(rec.withReason(status.NoOpReason)).To(BeEmpty(), "a refused render is never a no-op")
		Expect(rec.withReason(status.ClaimConflictReason)).To(HaveLen(2))

		By("the render goes back to the storage class of the claim")
		params.Renderer = &stubRenderer{result: renderWithClassedClaim([]string{"cfk-a", "cfk-b"}, "cfk-data", "fast")}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		expectSameClaim(before)
		Expect(configMapExists("cfk-b")).To(BeTrue())
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	})

	It("keeps the claim under forceConflicts when spec.dataPolicy is Keep", func() {
		nn := forceInstance("claims-force-keep-explicit", releasesv1alpha1.DataPolicyKeep)
		DeferCleanup(func() {
			removeClaims("cfe-data")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderWithClassedClaim(nil, "cfe-data", "fast")}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		before := settledClaim("cfe-data")
		rec.events = nil

		params.Renderer = &stubRenderer{result: renderWithClassedClaim(nil, "cfe-data", "slow")}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectSameClaim(before)
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		expectClaimConflict(mi.Status.Conditions, rec, "cfe-data")
	})

	It("recreates the claim under forceConflicts when spec.dataPolicy is Delete, without spec.prune", func() {
		nn := forceInstance("claims-force-delete", releasesv1alpha1.DataPolicyDelete)
		DeferCleanup(func() {
			removeClaims("cfd-data")
			removeConfigMaps("cfd-a")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: renderWithClassedClaim([]string{"cfd-a"}, "cfd-data", "fast")}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		before := settledClaim("cfd-data")
		rec.events = nil

		params.Renderer = &stubRenderer{result: renderWithClassedClaim([]string{"cfd-a"}, "cfd-data", "slow")}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectClaimRecreated(before, "slow")
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(rec.withReason(status.ClaimConflictReason)).To(BeEmpty())
		Expect(rec.withReason(status.AppliedReason)).To(HaveLen(1))
	})
})

// readySource serves one ready OCIRepository named name from memory, so that a
// ModulePackage reconcile resolves its source without the Flux CRDs.
func readySource(inner client.WithWatch, name string) client.Client {
	return interceptor.NewClient(inner, interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
		) error {
			repo, ok := obj.(*sourcev1.OCIRepository)
			if !ok || key.Name != name {
				return c.Get(ctx, key, obj, opts...)
			}
			repo.ObjectMeta = metav1.ObjectMeta{Name: name, Namespace: key.Namespace}
			repo.Status = sourcev1.OCIRepositoryStatus{
				Conditions: []metav1.Condition{{
					Type: fluxmeta.ReadyCondition, Status: metav1.ConditionTrue,
					Reason: "Succeeded", LastTransitionTime: metav1.Now(),
				}},
				Artifact: &fluxmeta.Artifact{
					URL: "http://source-controller/artifact.tar.gz", Revision: "main@sha256:aaa",
					Digest: "sha256:aaa", Path: "a.tar.gz", LastUpdateTime: metav1.Now(),
				},
			}
			return nil
		},
	})
}

// packageDirFetcher writes an instance.cue where the reconciler looks for the
// package; the stub renderer never reads it.
type packageDirFetcher struct{ path string }

func (f packageDirFetcher) Fetch(_ context.Context, _, _, dir string, _ opmsource.FetchOptions) error {
	pkgDir := filepath.Join(dir, f.path)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pkgDir, "instance.cue"), []byte("package app\n"), 0o600)
}

// stubPackageRenderer returns a copy of result as a ModuleInstance package.
type stubPackageRenderer struct{ result *render.RenderResult }

func (s *stubPackageRenderer) Render(context.Context, string) (string, *render.RenderResult, error) {
	r := *s.result
	return render.KindModuleInstance, &r, nil
}

var _ = Describe("PersistentVolumeClaims of a ModulePackage", func() {
	const sourceName = "claims-src"
	var (
		rec    *actionRecorder
		params *opmreconcile.ModulePackageParams
	)

	BeforeEach(func() {
		rec = &actionRecorder{}
		realClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		params = &opmreconcile.ModulePackageParams{
			Client:          readySource(realClient, sourceName),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   rec,
			Fetcher:         packageDirFetcher{path: "releases/app"},
		}
	})

	createPackage := func(name string, policy releasesv1alpha1.DataPolicy) types.NamespacedName {
		GinkgoHelper()
		pkg := &releasesv1alpha1.ModulePackage{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: releasesv1alpha1.ModulePackageSpec{
				SourceRef:  releasesv1alpha1.SourceReference{Kind: opmsource.SourceKindOCIRepository, Name: sourceName},
				Path:       "releases/app",
				Interval:   metav1.Duration{Duration: time.Minute},
				Prune:      true,
				DataPolicy: policy,
			},
		}
		Expect(k8sClient.Create(ctx, pkg)).To(Succeed())
		nn := types.NamespacedName{Name: name, Namespace: namespace}
		result, err := opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{Requeue: true}), "the first reconcile registers the finalizer")
		return nn
	}

	reconcilePackage := func(nn types.NamespacedName, r *render.RenderResult) {
		GinkgoHelper()
		params.Renderer = &stubPackageRenderer{result: r}
		_, err := opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
	}

	cleanupPackage := func(nn types.NamespacedName) {
		GinkgoHelper()
		var pkg releasesv1alpha1.ModulePackage
		if err := k8sClient.Get(ctx, nn, &pkg); err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			return
		}
		pkg.Finalizers = nil
		Expect(k8sClient.Update(ctx, &pkg)).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &pkg))).To(Succeed())
	}

	expectPackageGone := func(nn types.NamespacedName) {
		GinkgoHelper()
		err := k8sClient.Get(ctx, nn, &releasesv1alpha1.ModulePackage{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the ModulePackage must be gone, got %v", err)
	}

	It("keeps a stale claim by default and drops it from the inventory", func() {
		nn := createPackage("pkg-claims-prune-keep", "")
		DeferCleanup(func() {
			removeClaims("ppk-data")
			removeConfigMaps("ppk-a", "ppk-b")
			cleanupPackage(nn)
		})

		reconcilePackage(nn, renderOf([]string{"ppk-a", "ppk-b"}, []string{"ppk-data"}))
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		Expect(inventoryNames(pkg.Status.Inventory)).To(ConsistOf(
			"ConfigMap/ppk-a", "ConfigMap/ppk-b", "PersistentVolumeClaim/ppk-data"))

		reconcilePackage(nn, renderOf([]string{"ppk-a"}, nil))

		expectClaimUntouched("ppk-data")
		expectConfigMapGone("ppk-b")
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		Expect(inventoryNames(pkg.Status.Inventory)).To(ConsistOf("ConfigMap/ppk-a"))
		ready := apimeta.FindStatusCondition(pkg.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		kept := rec.withReason(status.ClaimsKeptReason)
		Expect(kept).To(HaveLen(1))
		Expect(kept[0].action).To(Equal("Prune"))
		Expect(kept[0].note).To(ContainSubstring(namespace + "/ppk-data"))
	})

	It("prunes a stale claim when spec.dataPolicy is Delete", func() {
		nn := createPackage("pkg-claims-prune-delete", releasesv1alpha1.DataPolicyDelete)
		DeferCleanup(func() {
			removeClaims("ppd-data")
			removeConfigMaps("ppd-a")
			cleanupPackage(nn)
		})

		reconcilePackage(nn, renderOf([]string{"ppd-a"}, []string{"ppd-data"}))
		reconcilePackage(nn, renderOf([]string{"ppd-a"}, nil))

		expectClaimDeleted("ppd-data")
		Expect(rec.withReason(status.ClaimsKeptReason)).To(BeEmpty())
	})

	It("keeps claims when the package is deleted, and the delete completes", func() {
		nn := createPackage("pkg-claims-delete-keep", "")
		DeferCleanup(func() {
			removeClaims("pdk-data")
			removeConfigMaps("pdk-a")
			cleanupPackage(nn)
		})

		reconcilePackage(nn, renderOf([]string{"pdk-a"}, []string{"pdk-data"}))
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &pkg)).To(Succeed())
		rec.events = nil
		reconcilePackage(nn, renderOf(nil, nil))

		expectPackageGone(nn)
		expectConfigMapGone("pdk-a")
		expectClaimUntouched("pdk-data")
		kept := rec.withReason(status.ClaimsKeptReason)
		Expect(kept).To(HaveLen(1))
		Expect(kept[0].action).To(Equal("Delete"))
	})

	It("deletes claims with the package when spec.dataPolicy is Delete", func() {
		nn := createPackage("pkg-claims-delete-delete", releasesv1alpha1.DataPolicyDelete)
		DeferCleanup(func() {
			removeClaims("pdd-data")
			removeConfigMaps("pdd-a")
			cleanupPackage(nn)
		})

		reconcilePackage(nn, renderOf([]string{"pdd-a"}, []string{"pdd-data"}))
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &pkg)).To(Succeed())
		rec.events = nil
		reconcilePackage(nn, renderOf(nil, nil))

		expectPackageGone(nn)
		expectClaimDeleted("pdd-data")
		Expect(rec.withReason(status.ClaimsKeptReason)).To(BeEmpty())
	})

	forcePackage := func(name string, policy releasesv1alpha1.DataPolicy) types.NamespacedName {
		GinkgoHelper()
		nn := createPackage(name, policy)
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		pkg.Spec.Rollout = &releasesv1alpha1.RolloutSpec{ForceConflicts: true}
		Expect(k8sClient.Update(ctx, &pkg)).To(Succeed())
		return nn
	}

	It("keeps a claim with a changed immutable field under forceConflicts and reports a conflict", func() {
		nn := forcePackage("pkg-claims-force-keep", "")
		DeferCleanup(func() {
			removeClaims("pfk-data")
			removeConfigMaps("pfk-a", "pfk-b")
			cleanupPackage(nn)
		})

		reconcilePackage(nn, renderWithClassedClaim([]string{"pfk-a"}, "pfk-data", "fast"))
		before := settledClaim("pfk-data")
		rec.events = nil

		params.Renderer = &stubPackageRenderer{result: renderWithClassedClaim([]string{"pfk-a", "pfk-b"}, "pfk-data", "slow")}
		result, err := opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "the conflict is retried on the backoff")

		expectSameClaim(before)
		expectConfigMapGone("pfk-b", "no object of the refused render may be applied")
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		expectClaimConflict(pkg.Status.Conditions, rec, "pfk-data")
		Expect(inventoryNames(pkg.Status.Inventory)).To(ConsistOf("ConfigMap/pfk-a", "PersistentVolumeClaim/pfk-data"))
	})

	It("recreates the claim under forceConflicts when spec.dataPolicy is Delete", func() {
		nn := forcePackage("pkg-claims-force-delete", releasesv1alpha1.DataPolicyDelete)
		DeferCleanup(func() {
			removeClaims("pfd-data")
			cleanupPackage(nn)
		})

		reconcilePackage(nn, renderWithClassedClaim(nil, "pfd-data", "fast"))
		before := settledClaim("pfd-data")
		rec.events = nil

		reconcilePackage(nn, renderWithClassedClaim(nil, "pfd-data", "slow"))

		expectClaimRecreated(before, "slow")
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		ready := apimeta.FindStatusCondition(pkg.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(rec.withReason(status.ClaimConflictReason)).To(BeEmpty())
	})
})
