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
	"github.com/open-platform-model/opm-operator/internal/render"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// unknownKindRenderResult builds a render result whose single resource has a
// GVK the cluster does not serve, so the apply phase fails with a transient
// (non-stalled) error — the staging hook for blocked→Ready recovery tests.
func unknownKindRenderResult(namespace string) *render.RenderResult {
	cueCtx := cuecontext.New()
	obj := cueCtx.CompileString(fmt.Sprintf(`{
	apiVersion: "test.example.com/v1"
	kind:       "Phantom"
	metadata: {
		name:      "phantom-1"
		namespace: %q
		labels: {
			%q: %q
			%q: %q
		}
	}
}`, namespace,
		core.LabelManagedBy, core.LabelManagedByControllerValue,
		core.LabelModuleInstanceUUID, stubInstanceUUID))
	if obj.Err() != nil {
		panic(fmt.Sprintf("compiling phantom stub resource: %v", obj.Err()))
	}

	resource := &core.Resource{
		Value:       obj,
		Instance:    "phantom-1",
		Component:   "phantom",
		Transformer: "kubernetes#simple",
	}
	u, err := resource.ToUnstructured()
	if err != nil {
		panic(fmt.Sprintf("converting phantom stub resource: %v", err))
	}

	return &render.RenderResult{
		Resources:        []*core.Resource{resource},
		InventoryEntries: []releasesv1alpha1.InventoryEntry{inventory.NewEntryFromResource(u)},
	}
}

var _ = Describe("Reconcile State Recovery", func() {
	// Validates Stalled → Ready recovery once the module resolves again
	// (design 3.1; resolution failure staged via the suite's error renderers).
	It("should recover from Stalled when the source becomes available", func() {
		mrName := "stalled-recover-mr"
		createModuleInstance(mrName)
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		params.Renderer = resolutionErrorRenderer()
		ensureFinalizer(params, nn)

		By("first reconcile stalls on module resolution")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred(), "stalled failures return nil error")
		Expect(result.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))

		var stalledMI releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &stalledMI)).To(Succeed())
		stalled := apimeta.FindStatusCondition(stalledMI.Status.Conditions, status.StalledCondition)
		Expect(stalled).NotTo(BeNil())
		Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
		Expect(stalled.Reason).To(Equal(status.ResolutionFailedReason))
		ready := apimeta.FindStatusCondition(stalledMI.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))

		By("second reconcile recovers once the module resolves")
		params.Renderer = &stubRenderer{}
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var recovered releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &recovered)).To(Succeed())
		ready = apimeta.FindStatusCondition(recovered.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(apimeta.FindStatusCondition(recovered.Status.Conditions, status.StalledCondition)).To(BeNil(),
			"Stalled condition is cleared on recovery")

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())

		By("history reflects the failure → success transition")
		Expect(len(recovered.Status.History)).To(BeNumerically(">=", 2))
		Expect(recovered.Status.History[0].Phase).To(Equal("complete"))
		Expect(recovered.Status.History[1].Message).To(ContainSubstring("module not found"))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
		cleanupInstance(nn)
	})

	// Validates transient-blocked → Ready recovery: a failing apply keeps the
	// instance on the fast exponential backoff (not the 30-minute stalled
	// recheck) until the cause clears (design 3.2 — the OCIRepository
	// SourceReady staging is retired design; an unserved GVK stages the
	// equivalent transient block in the current pipeline).
	It("should recover from a transient blocked state when the cause clears", func() {
		mrName := "blocked-recover-mr"
		createModuleInstance(mrName)
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		params.Renderer = &stubRenderer{result: unknownKindRenderResult(namespace)}
		ensureFinalizer(params, nn)

		By("first reconcile fails the apply transiently")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred(), "transient failures return nil error with backoff")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		Expect(result.RequeueAfter).To(BeNumerically("<=", opmreconcile.BackoffMaxDelay),
			"transient failures use the fast exponential backoff, not the stalled recheck")

		var blocked releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &blocked)).To(Succeed())
		ready := apimeta.FindStatusCondition(blocked.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyFailedReason))
		Expect(apimeta.FindStatusCondition(blocked.Status.Conditions, status.StalledCondition)).To(BeNil(),
			"transient failures do not stall")
		Expect(blocked.Status.NextRetryAt).NotTo(BeNil())

		By("second reconcile recovers once the render output is applicable")
		params.Renderer = &stubRenderer{}
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var recovered releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &recovered)).To(Succeed())
		ready = apimeta.FindStatusCondition(recovered.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(recovered.Status.NextRetryAt).To(BeNil(), "nextRetryAt is cleared on success")

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())

		// Cleanup
		Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
		cleanupInstance(nn)
	})

	// Validates suspend → unsuspend resumes the full reconcile at the
	// reconcile-loop level (design 3.3).
	It("should resume reconciliation when suspend is cleared", func() {
		mrName := "suspend-recover-mr"
		mr := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mrName,
				Namespace: namespace,
			},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Suspend: true,
				Module: releasesv1alpha1.ModuleReference{
					Path:    "opmodel.dev/test/module",
					Version: "v0.1.0",
				},
				Prune:  true,
				Values: &releasesv1alpha1.RawValues{},
			},
		}
		mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
		Expect(k8sClient.Create(ctx, mr)).To(Succeed())
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		ensureFinalizer(params, nn)

		By("reconcile while suspended returns early without applying")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		var suspended releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &suspended)).To(Succeed())
		ready := apimeta.FindStatusCondition(suspended.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.SuspendedReason))

		var cm corev1.ConfigMap
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)
		Expect(err).To(HaveOccurred(), "no resources applied while suspended")

		By("clearing suspend resumes the full reconcile")
		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		current.Spec.Suspend = false
		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var resumed releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &resumed)).To(Succeed())
		ready = apimeta.FindStatusCondition(resumed.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).NotTo(Equal(status.SuspendedReason))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("hello"))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
		cleanupInstance(nn)
	})
})
