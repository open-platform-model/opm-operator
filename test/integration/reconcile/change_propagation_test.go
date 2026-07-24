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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

var _ = Describe("Reconcile Change Propagation", func() {
	// Validates that a spec.values change escapes Phase 4 no-op detection:
	// the config digest differs, Phase 3 re-renders, and Phase 5 re-applies
	// (design 1.1).
	It("should re-apply when spec.values changes", func() {
		mrName := "values-change-mr"
		createModuleInstance(mrName) // values: {"message": "hello"}
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		ensureFinalizer(params, nn)

		By("first reconcile applies the initial values")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("hello"))

		var first releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &first)).To(Succeed())
		firstConfigDigest := first.Status.LastAppliedConfigDigest
		Expect(firstConfigDigest).NotTo(BeEmpty())
		Expect(first.Status.Inventory).NotTo(BeNil())
		firstRevision := first.Status.Inventory.Revision
		firstHistoryLen := len(first.Status.History)

		By("patching spec.values")
		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		current.Spec.Values.Raw = []byte(`{"message": "updated"}`)
		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		By("second reconcile re-renders and re-applies the new values")
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("updated"))

		var second releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &second)).To(Succeed())
		Expect(second.Status.LastAppliedConfigDigest).NotTo(Equal(firstConfigDigest),
			"config digest must change when spec.values changes")
		Expect(second.Status.Inventory.Revision).To(Equal(firstRevision+1),
			"inventory revision bumps on re-apply")
		Expect(second.Status.History).To(HaveLen(firstHistoryLen + 1))
		Expect(second.Status.History[0].Phase).To(Equal("complete"))
		Expect(second.Status.History[1].Phase).To(Equal("complete"))

		ready := apimeta.FindStatusCondition(second.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
		})).To(Succeed())
		cleanupInstance(nn)
	})

	// Validates source-change propagation. The current pipeline derives the
	// source digest from spec.module path+version (no Flux artifact), so a
	// module version bump is the source-revision change: the source digest
	// differs, no-op detection is escaped, and the full pipeline re-executes
	// with the new module content (design 1.2).
	It("should re-apply when source revision changes", func() {
		mrName := "source-rev-mr"
		createModuleInstance(mrName)
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		ensureFinalizer(params, nn)

		By("first reconcile applies content rendered from module v0.1.0")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var first releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &first)).To(Succeed())
		firstSourceDigest := first.Status.LastAppliedSourceDigest
		Expect(firstSourceDigest).NotTo(BeEmpty())

		By("bumping spec.module.version — the new module version renders new content")
		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		current.Spec.Module.Version = "v0.2.0"
		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		// Simulate the new module version producing different content for the
		// same values (spec.values still says "hello").
		newContent := &releasesv1alpha1.RawValues{}
		newContent.Raw = []byte(`{"message": "from-v2"}`)
		params.Renderer = &stubRenderer{result: stubRenderResult(namespace, newContent)}

		By("second reconcile re-executes the full pipeline")
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var second releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &second)).To(Succeed())
		Expect(second.Status.LastAppliedSourceDigest).NotTo(Equal(firstSourceDigest),
			"source digest must change when the module version changes")
		Expect(second.Status.LastAppliedSourceDigest).To(Equal(second.Status.LastAttemptedSourceDigest))

		By("the managed resource reflects the new module content")
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("from-v2"))

		ready := apimeta.FindStatusCondition(second.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
		})).To(Succeed())
		cleanupInstance(nn)
	})
})
