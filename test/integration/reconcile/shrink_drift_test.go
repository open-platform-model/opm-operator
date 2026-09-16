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
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// driftCondition returns the instance's Drifted condition, or nil.
func driftCondition(nn types.NamespacedName) *metav1.Condition {
	var mi releasesv1alpha1.ModuleInstance
	Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
	return apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
}

// mutateSidecar changes the applied ConfigMap out of band, which is genuine
// drift: the cluster moved away from what the operator asserts.
func mutateSidecar(name string) {
	var cm corev1.ConfigMap
	nn := types.NamespacedName{Name: name, Namespace: namespace}
	Expect(k8sClient.Get(ctx, nn, &cm)).To(Succeed())
	cm.Data["payload"] = "tampered"
	Expect(k8sClient.Update(ctx, &cm)).To(Succeed())
}

var _ = Describe("Reconcile Withheld Resource Drift", func() {
	// registration-shrink-refusal section 3: drift means the cluster diverged
	// from what the operator asserts. A withheld resource is one the operator
	// is deliberately not asserting, so reporting it would name a difference
	// the operator created on purpose and will never close — and would bury
	// real drift on the same instance behind a condition that never clears.
	It("does not report a withheld resource as drift", func() {
		providerName := "drift-withheld-mr"
		claimName := namespace + "." + providerName
		consumer := demandContracts("drift-withheld-consumer-mr", contractBackup)

		storeClaim(claimName, providerName, contractStorage, contractBackup)
		createModuleInstance(providerName)
		nn := types.NamespacedName{Name: providerName, Namespace: namespace}

		params := reconcileParams()
		params.EventRecorder = events.NewFakeRecorder(30)
		params.APIReader = k8sClient
		params.Renderer = &stubRenderer{result: providerRenderResult(
			claimName, providerName, contractStorage,
		)}
		ensureFinalizer(params, nn)

		By("two refused reconciles: the second sees the withheld claim against a live one that differs")
		for range 2 {
			result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		}

		By("nothing else diverged, so Drifted is not set by the refusal")
		drifted := driftCondition(nn)
		if drifted != nil {
			Expect(drifted.Status).To(Equal(metav1.ConditionFalse),
				"the withheld claim set Drifted: %s", drifted.Message)
		}

		cleanupShrinkFixtures(claimName, providerName, nn, consumer)
	})

	// The exclusion must not swallow drift that is real.
	It("still reports drift on another resource while one is withheld", func() {
		providerName := "drift-alongside-mr"
		claimName := namespace + "." + providerName
		consumer := demandContracts("drift-alongside-consumer-mr", contractBackup)

		storeClaim(claimName, providerName, contractStorage, contractBackup)
		createModuleInstance(providerName)
		nn := types.NamespacedName{Name: providerName, Namespace: namespace}

		params := reconcileParams()
		params.EventRecorder = events.NewFakeRecorder(30)
		params.APIReader = k8sClient
		params.Renderer = &stubRenderer{result: providerRenderResult(
			claimName, providerName, contractStorage,
		)}
		ensureFinalizer(params, nn)

		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("the applied sidecar is tampered with out of band")
		mutateSidecar(providerName + "-cm")

		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("drift names the tampered resource and only it")
		drifted := driftCondition(nn)
		Expect(drifted).NotTo(BeNil())
		Expect(drifted.Status).To(Equal(metav1.ConditionTrue))
		Expect(drifted.Message).To(ContainSubstring("1 resource(s)"))

		cleanupShrinkFixtures(claimName, providerName, nn, consumer)
	})

	// Exclusion is a property of being withheld, not of being a claim.
	It("includes a resource in drift detection once it stops being withheld", func() {
		providerName := "drift-returns-mr"
		claimName := namespace + "." + providerName
		consumer := demandContracts("drift-returns-consumer-mr", contractBackup)

		storeClaim(claimName, providerName, contractStorage, contractBackup)
		createModuleInstance(providerName)
		nn := types.NamespacedName{Name: providerName, Namespace: namespace}

		params := reconcileParams()
		params.EventRecorder = events.NewFakeRecorder(30)
		params.APIReader = k8sClient
		params.Renderer = &stubRenderer{result: providerRenderResult(
			claimName, providerName, contractStorage,
		)}
		ensureFinalizer(params, nn)

		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("the dependent releases the contract, so the claim is applied from now on")
		var dependent releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, consumer, &dependent)).To(Succeed())
		dependent.Status.RequiredContracts = nil
		Expect(k8sClient.Status().Update(ctx, &dependent)).To(Succeed())

		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("the claim is tampered with out of band")
		var claim releasesv1alpha1.TransformerRegistration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claimName}, &claim)).To(Succeed())
		claim.Spec.Version = "9.9.9"
		Expect(k8sClient.Update(ctx, &claim)).To(Succeed())

		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("the claim is back in the comparison and its drift is reported")
		drifted := driftCondition(nn)
		Expect(drifted).NotTo(BeNil())
		Expect(drifted.Status).To(Equal(metav1.ConditionTrue))

		cleanupShrinkFixtures(claimName, providerName, nn, consumer)
	})
})
