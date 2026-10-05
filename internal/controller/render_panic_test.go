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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// panicMessage is what the deferred commit writes for the panicking
// renderers in render_slots_test.go.
const panicMessage = "reconcile panicked: render blew up"

// expectPanicRecorded asserts the condition shape, history entry, counter and
// retry time a recovered panic leaves behind.
func expectPanicRecorded(
	nn types.NamespacedName,
	conditions []metav1.Condition,
	history []releasesv1alpha1.HistoryEntry,
	counters *releasesv1alpha1.FailureCounters,
	nextRetryAt *metav1.Time,
) {
	ready := apimeta.FindStatusCondition(conditions, status.ReadyCondition)
	Expect(ready).NotTo(BeNil(), "%s has a Ready condition", nn)
	Expect(ready.Status).To(Equal(metav1.ConditionFalse), "%s: reason=%s message=%s", nn, ready.Reason, ready.Message)
	Expect(ready.Reason).To(Equal(status.ReconcilePanicReason))
	Expect(ready.Message).To(ContainSubstring(panicMessage))

	reconciling := apimeta.FindStatusCondition(conditions, status.ReconcilingCondition)
	Expect(reconciling).NotTo(BeNil(), "%s has a Reconciling condition", nn)
	Expect(reconciling.Status).To(Equal(metav1.ConditionTrue))
	Expect(apimeta.FindStatusCondition(conditions, status.StalledCondition)).To(BeNil(), "a panic is not stalled")

	Expect(counters).NotTo(BeNil())
	Expect(counters.Reconcile).To(Equal(int64(1)), "the panic counts as one failed reconcile")
	Expect(history).NotTo(BeEmpty())
	Expect(history[0].Message).To(ContainSubstring(panicMessage), "the newest history entry is the failure")
	Expect(history[0].Phase).NotTo(Equal("complete"), "the newest history entry is not a success")
	Expect(nextRetryAt).To(BeNil(), "the operator schedules no retry of its own")
}

var _ = Describe("Render panic", func() {
	It("does not mark a ready ModuleInstance Ready when its render panics", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "panic-mi")
		nn := createRenderTestInstance(ctx, ns, "panic-mi")

		r := &ModuleInstanceReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Renderer:        &stubRenderer{},
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn}) // adds the finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var before releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
		expectReady(nn, before.Status.Conditions)
		Expect(before.Status.Inventory).NotTo(BeNil())

		r.Renderer = panickingModuleRenderer{}
		Expect(func() {
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		}).To(PanicWith("render blew up"))

		var after releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &after)).To(Succeed())
		expectPanicRecorded(nn, after.Status.Conditions, after.Status.History, after.Status.FailureCounters, after.Status.NextRetryAt)
		Expect(after.Status.Inventory).To(Equal(before.Status.Inventory), "the inventory keeps the last success")
		Expect(after.Status.LastAppliedRenderDigest).To(Equal(before.Status.LastAppliedRenderDigest))
	})

	It("does not mark a ready ModulePackage Ready when its render panics", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "panic-mp")
		nn := createRenderTestPackage(ctx, ns, "panic-mp")

		r := &ModulePackageReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Fetcher:         &stubFetcher{pathInArtifact: renderTestPath},
			Renderer:        &stubPackageRenderer{result: stubRenderResult(ns, nil)},
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn}) // adds the finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var before releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
		expectReady(nn, before.Status.Conditions)
		Expect(before.Status.Inventory).NotTo(BeNil())

		r.Renderer = panickingPackageRenderer{}
		Expect(func() {
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		}).To(PanicWith("render blew up"))

		var after releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &after)).To(Succeed())
		expectPanicRecorded(nn, after.Status.Conditions, after.Status.History, after.Status.FailureCounters, after.Status.NextRetryAt)
		Expect(after.Status.Inventory).To(Equal(before.Status.Inventory), "the inventory keeps the last success")
		Expect(after.Status.LastAppliedRenderDigest).To(Equal(before.Status.LastAppliedRenderDigest))
	})
})
