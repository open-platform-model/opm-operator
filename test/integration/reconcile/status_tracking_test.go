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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// immutableConfigMapRenderResult builds a render result of a single immutable
// ConfigMap. Changing its data on a later reconcile triggers the API server's
// immutable-field rejection, which only ApplyOptions.Force (driven by
// spec.rollout.forceConflicts) resolves via delete-and-recreate.
func immutableConfigMapRenderResult(namespace, name, message string) *render.RenderResult {
	cueCtx := cuecontext.New()
	cm := cueCtx.CompileString(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
			%q: %q
		}
	}
	immutable: true
	data: {
		message: %q
	}
}`, name, namespace,
		core.LabelManagedBy, core.LabelManagedByControllerValue,
		core.LabelModuleInstanceNamespace, namespace,
		core.LabelModuleInstanceUUID, stubInstanceUUID,
		message))
	if cm.Err() != nil {
		panic(fmt.Sprintf("compiling immutable stub ConfigMap: %v", cm.Err()))
	}

	resource := &core.Resource{
		Value:       cm,
		Instance:    name,
		Component:   name,
		Transformer: "kubernetes#simple",
	}
	u, err := resource.ToUnstructured()
	if err != nil {
		panic(fmt.Sprintf("converting immutable stub resource: %v", err))
	}

	return &render.RenderResult{
		Resources:        []*core.Resource{resource},
		InventoryEntries: []releasesv1alpha1.InventoryEntry{inventory.NewEntryFromResource(u)},
	}
}

var _ = Describe("Reconcile Status Tracking", func() {
	// Validates Status.ObservedGeneration tracks the reconciled generation
	// across spec changes (design 4.1).
	It("should update ObservedGeneration across spec changes", func() {
		mrName := "obsgen-mr"
		createModuleInstance(mrName)
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		ensureFinalizer(params, nn)

		By("first reconcile stamps the initial generation")
		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var first releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &first)).To(Succeed())
		firstGeneration := first.Generation
		Expect(first.Status.ObservedGeneration).To(Equal(firstGeneration))

		By("patching spec.values bumps the generation")
		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		current.Spec.Values.Raw = []byte(`{"message": "generation-two"}`)
		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		By("second reconcile stamps the new generation")
		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var second releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &second)).To(Succeed())
		Expect(second.Generation).To(Equal(firstGeneration + 1))
		Expect(second.Status.ObservedGeneration).To(Equal(second.Generation))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
		})).To(Succeed())
		cleanupInstance(nn)
	})

	// Validates history entries across success → failure → success with
	// monotonic sequences and newest-first ordering (design 4.2).
	It("should record history across mixed success and failure outcomes", func() {
		mrName := "history-mix-mr"
		createModuleInstance(mrName)
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		ensureFinalizer(params, nn)

		By("first reconcile succeeds")
		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var first releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &first)).To(Succeed())
		Expect(first.Status.History).To(HaveLen(1))
		Expect(first.Status.History[0].Phase).To(Equal("complete"))
		Expect(first.Status.History[0].Sequence).To(Equal(int64(1)))

		By("second reconcile fails on an injected fetch error")
		params.Renderer = renderErrorRenderer("injected fetch failure")
		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred(), "stalled failures return nil error")

		var second releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &second)).To(Succeed())
		Expect(second.Status.History).To(HaveLen(2))
		Expect(second.Status.History[0].Sequence).To(Equal(int64(2)))
		Expect(second.Status.History[0].Message).To(ContainSubstring("injected fetch failure"))
		Expect(second.Status.History[1].Sequence).To(Equal(int64(1)))
		Expect(second.Status.History[1].Phase).To(Equal("complete"))

		By("third reconcile succeeds after clearing the error")
		// Restore the working renderer and change spec.values — with unchanged
		// digests the recovery reconcile would be a NoOp, which by design
		// records no history entry.
		params.Renderer = &stubRenderer{}
		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		current.Spec.Values.Raw = []byte(`{"message": "recovered"}`)
		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var third releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &third)).To(Succeed())
		Expect(third.Status.History).To(HaveLen(3))
		Expect(len(third.Status.History)).To(BeNumerically("<=", status.MaxHistoryEntries))
		Expect(third.Status.History[0].Sequence).To(Equal(int64(3)))
		Expect(third.Status.History[0].Phase).To(Equal("complete"))
		Expect(third.Status.History[1].Message).To(ContainSubstring("injected fetch failure"))
		Expect(third.Status.History[2].Sequence).To(Equal(int64(1)))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
		})).To(Succeed())
		cleanupInstance(nn)
	})

	// Validates spec.rollout.forceConflicts propagates to the apply layer
	// (design 4.3). Flux always applies with SSA ForceOwnership, so the force
	// flag's observable behavior is immutable-field recreation: changing an
	// immutable ConfigMap fails without force and succeeds (delete-and-
	// recreate) with force.
	It("should honor spec.rollout.forceConflicts during apply", func() {
		mrName := "force-conflicts-mr"
		createModuleInstance(mrName)
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		params.Renderer = &stubRenderer{result: immutableConfigMapRenderResult(namespace, "force-cm", "v1")}
		ensureFinalizer(params, nn)

		By("first reconcile applies the immutable ConfigMap")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "force-cm", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("v1"))

		By("changing the immutable data without forceConflicts fails the apply")
		params.Renderer = &stubRenderer{result: immutableConfigMapRenderResult(namespace, "force-cm", "v2")}
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred(), "transient apply failure returns nil error with backoff")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		var failed releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &failed)).To(Succeed())
		ready := apimeta.FindStatusCondition(failed.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyFailedReason))

		// Live object still has the original data.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "force-cm", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("v1"))

		By("enabling spec.rollout.forceConflicts recreates the immutable object")
		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		current.Spec.Rollout = &releasesv1alpha1.RolloutSpec{ForceConflicts: true}
		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "force-cm", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("v2"))

		var forced releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &forced)).To(Succeed())
		ready = apimeta.FindStatusCondition(forced.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "force-cm", Namespace: namespace},
		})).To(Succeed())
		cleanupInstance(nn)
	})

	// TODO (design 4.4): cross-namespace sourceRef resolution. Explicitly left
	// for the active `add-cross-namespace-source-grants` change, which owns
	// this behavior (see openspec/changes/add-cross-namespace-source-grants).
	It("should resolve sourceRef across namespaces", func() {
		Skip("owned by the add-cross-namespace-source-grants change")
	})
})
