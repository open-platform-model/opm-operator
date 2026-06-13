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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/runtime/patch"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// failureReconciler builds a PlatformReconciler with a concrete *FakeRecorder so
// the test can inspect emitted events, and exercises the failure path directly
// (no live registry — the failMaterialize helper carries all the novel
// retry/observed-generation/event-gating behavior under test).
func failureReconciler(store *platformstore.Store) (*PlatformReconciler, *events.FakeRecorder) {
	recorder := events.NewFakeRecorder(10)
	return &PlatformReconciler{
		Client:        k8sClient,
		Scheme:        runtime.NewScheme(), // unused on the failure path
		EventRecorder: recorder,
		Store:         store,
	}, recorder
}

// createSingleton creates the cluster Platform and returns it with Generation
// populated.
func createSingleton() *releasesv1alpha1.Platform {
	plat := &releasesv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
		Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
	}
	Expect(k8sClient.Create(ctx, plat)).To(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
	return plat
}

// freshPatcher re-fetches the singleton (modelling the start of a reconcile) and
// returns it with a serial patcher snapshotting the pre-reconcile status.
func freshPatcher(plat *releasesv1alpha1.Platform) *patch.SerialPatcher {
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
	return patch.NewSerialPatcher(plat, k8sClient)
}

var _ = Describe("Platform Controller failure handling", func() {
	AfterEach(func() {
		deletePlatform(platformSingletonName)
	})

	It("requeues a transient failure on the short interval with Ready=False/MaterializeFailed", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		patcher := freshPatcher(plat)

		// A deadline-exceeded cause classifies as transient.
		res, err := r.failMaterialize(ctx, patcher, plat, context.DeadlineExceeded, "materialize failed: registry timed out")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(transientRecheckInterval))

		fetched := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
		ready := apimeta.FindStatusCondition(fetched.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.MaterializeFailedReason))
	})

	It("requeues a semantic/unclassifiable failure on the long stalled interval", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		patcher := freshPatcher(plat)

		// A plain error cannot be classified as transient → long interval.
		res, err := r.failMaterialize(ctx, patcher, plat, errors.New("subscription path could not be resolved"), "materialize failed: bad subscription path")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))
	})

	It("records observedGeneration on the failure path", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		patcher := freshPatcher(plat)

		_, err := r.failMaterialize(ctx, patcher, plat, errors.New("boom"), "materialize failed: boom")
		Expect(err).NotTo(HaveOccurred())

		fetched := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
		Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
		Expect(fetched.Status.ObservedGeneration).NotTo(BeZero())
	})

	It("does not re-emit the warning event across repeated identical failures", func() {
		r, recorder := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		const msg = "materialize failed: identical cause"

		// First reconcile: enters the failed state → one event.
		_, err := r.failMaterialize(ctx, freshPatcher(plat), plat, errors.New("boom"), msg)
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: same failure, unchanged message → no new event.
		_, err = r.failMaterialize(ctx, freshPatcher(plat), plat, errors.New("boom"), msg)
		Expect(err).NotTo(HaveOccurred())

		Expect(recorder.Events).To(HaveLen(1), "an unchanged failure must not re-emit the warning event on recheck")
	})

	It("preserves a previously stored good platform after a failed reconcile", func() {
		store := platformstore.NewStore()
		lastGood := materializeMarker()
		store.Set(7, lastGood)

		r, _ := failureReconciler(store)
		plat := createSingleton()
		patcher := freshPatcher(plat)

		_, err := r.failMaterialize(ctx, patcher, plat, errors.New("boom"), "materialize failed: boom")
		Expect(err).NotTo(HaveOccurred())

		held, ok := store.Get()
		Expect(ok).To(BeTrue(), "the last-good platform must survive a failed reconcile")
		Expect(held).To(BeIdenticalTo(lastGood))
		Expect(store.Generation()).To(Equal(int64(7)))
	})
})
