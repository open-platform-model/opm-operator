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

	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/open-platform-model/library/opm/platform"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/internal/version"
)

// failureReconciler builds a PlatformReconciler with a concrete *FakeRecorder so
// the test can inspect emitted events, and exercises the failure path directly
// (no live registry — the failReconcile helper carries all the novel
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
		deletePlatform()
	})

	It("requeues a transient failure on the short interval with Ready=False/BuildFailed", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		patcher := freshPatcher(plat)

		// A deadline-exceeded cause classifies as transient.
		res, err := r.failReconcile(ctx, patcher, plat, status.BuildFailedReason, context.DeadlineExceeded, "resolving platform dependencies: registry timed out")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(transientRecheckInterval))

		fetched := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
		ready := apimeta.FindStatusCondition(fetched.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.BuildFailedReason))
	})

	It("requeues a semantic/unclassifiable failure on the long stalled interval", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		patcher := freshPatcher(plat)

		// A plain error cannot be classified as transient → long interval.
		res, err := r.failReconcile(ctx, patcher, plat, status.BuildFailedReason, errors.New("subscription path could not be resolved"), "resolving platform dependencies: bad subscription path")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))
	})

	It("records observedGeneration on the failure path", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		patcher := freshPatcher(plat)

		_, err := r.failReconcile(ctx, patcher, plat, status.GenerateFailedReason, errors.New("boom"), "writing platform module: boom")
		Expect(err).NotTo(HaveOccurred())

		fetched := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
		Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
		Expect(fetched.Status.ObservedGeneration).NotTo(BeZero())
	})

	It("stamps operatorVersion on the failure path", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		patcher := freshPatcher(plat)

		_, err := r.failReconcile(ctx, patcher, plat, status.GenerateFailedReason, errors.New("boom"), "writing platform module: boom")
		Expect(err).NotTo(HaveOccurred())

		fetched := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
		Expect(fetched.Status.OperatorVersion).To(Equal(version.Full()))
	})

	It("does not re-emit the warning event across repeated identical failures", func() {
		r, recorder := failureReconciler(platformstore.NewStore())
		plat := createSingleton()
		const msg = "building platform module: identical cause"

		// First reconcile: enters the failed state → one event.
		_, err := r.failReconcile(ctx, freshPatcher(plat), plat, status.BuildFailedReason, errors.New("boom"), msg)
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: same failure, unchanged message → no new event.
		_, err = r.failReconcile(ctx, freshPatcher(plat), plat, status.BuildFailedReason, errors.New("boom"), msg)
		Expect(err).NotTo(HaveOccurred())

		Expect(recorder.Events).To(HaveLen(1), "an unchanged failure must not re-emit the warning event on recheck")
	})

	It("preserves a previously stored good platform after a failed reconcile", func() {
		store := platformstore.NewStore()
		lastGood := generatedMarker(7)
		store.SetGenerated(lastGood)

		r, _ := failureReconciler(store)
		plat := createSingleton()
		patcher := freshPatcher(plat)

		_, err := r.failReconcile(ctx, patcher, plat, status.GenerateFailedReason, errors.New("boom"), "writing platform module: boom")
		Expect(err).NotTo(HaveOccurred())

		held, ok := store.Generated()
		Expect(ok).To(BeTrue(), "the last-good platform must survive a failed reconcile")
		Expect(held.Platform).To(BeIdenticalTo(lastGood.Platform))
		Expect(store.Identity()).To(Equal(platformIdentity(7)))
	})
})

// The inventory refusals go through failReconcile exactly as the reconciler
// routes them, with hand-built inventories: no published catalog pair is
// over-subscribed or undiscriminated, and a fixture catalog pair is not worth
// a second fixture kind in a shared publish pipeline for two messages
// (design.md § How the refusals are tested without a refusing catalog pair).
// The wording itself is pinned by the table tests in
// platform_inventory_test.go; these specs pin what a refusal does to the
// object and the store.
var _ = Describe("Platform Controller inventory refusals", func() {
	const priorReport = "the enabled catalogs define no contract, so nothing was verified"

	// lastGoodRegistry is the resolved registry a previous successful
	// generation left on status.
	lastGoodRegistry := []releasesv1alpha1.ResolvedRegistryEntry{{
		Catalog: opmCatalog,
		Version: "4.0.1",
		Enabled: true,
		Source:  releasesv1alpha1.RegistryEntrySubscription,
	}}

	// seedLastGoodStatus stamps the status a successful generation would have
	// left: the package identity, the resolved registry and a
	// ContractsFulfilled report, all describing the package renders are still
	// consuming. A refusal must leave every one of them exactly as it stands.
	seedLastGoodStatus := func(plat *releasesv1alpha1.Platform) {
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
		plat.Status.PackageIdentity = platformIdentity(7).String()
		plat.Status.Registry = lastGoodRegistry
		conditions.MarkTrue(plat, status.ContractsFulfilledCondition, status.NoContractsDefinedReason, "%s", priorReport)
		Expect(k8sClient.Status().Update(ctx, plat)).To(Succeed())
	}

	// refuse runs one reconcile's worth of the gate: the verdict the
	// reconciler would compute, routed through the helper it routes it
	// through, with the nil classify error a refusal carries.
	refuse := func(r *PlatformReconciler, plat *releasesv1alpha1.Platform, inv *platform.ContractInventory) ctrl.Result {
		reason, msg, refused := inventoryRefusal(inv)
		Expect(refused).To(BeTrue(), "the spec's inventory must be refused")
		res, err := r.failReconcile(ctx, freshPatcher(plat), plat, reason, nil, msg)
		Expect(err).NotTo(HaveOccurred())
		return res
	}

	overSubscribed := func() *platform.ContractInventory {
		return &platform.ContractInventory{
			DefinedBy:      map[string]string{backupTrait: opmCatalog},
			RequiredBy:     map[string][]string{backupTrait: {veleroSchedule, k8upSchedule}},
			OverSubscribed: []string{backupTrait},
			Routable:       false,
			Discriminated:  true,
		}
	}

	comparable := func() *platform.ContractInventory {
		return &platform.ContractInventory{
			DefinedBy:  map[string]string{containerResource: opmCatalog},
			RequiredBy: map[string][]string{containerResource: {mirrorTransformer, deployTransformer}},
			Comparable: []platform.ComparablePredicates{
				{Broader: mirrorTransformer, Narrower: deployTransformer, Contracts: []string{containerResource}},
			},
			Routable:      true,
			Discriminated: false,
		}
	}

	both := func() *platform.ContractInventory {
		inv := overSubscribed()
		inv.DefinedBy[containerResource] = opmCatalog
		inv.RequiredBy[containerResource] = []string{mirrorTransformer, deployTransformer}
		inv.Comparable = []platform.ComparablePredicates{
			{Broader: mirrorTransformer, Narrower: deployTransformer, Contracts: []string{containerResource}},
		}
		inv.Discriminated = false
		return inv
	}

	AfterEach(func() {
		deletePlatform()
	})

	It("refuses an over-subscribed platform naming the contract, its catalog and every requiring transformer", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()

		refuse(r, plat, overSubscribed())

		ready := readyCondition(fetchPlatform())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.OverSubscribedContractsReason))
		Expect(ready.Message).To(ContainSubstring(backupTrait))
		Expect(ready.Message).To(ContainSubstring("defined by " + opmCatalog))
		Expect(ready.Message).To(ContainSubstring(k8upSchedule))
		Expect(ready.Message).To(ContainSubstring(veleroSchedule))
	})

	It("refuses a comparable pair naming broader, narrower and the shared contract", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()

		refuse(r, plat, comparable())

		ready := readyCondition(fetchPlatform())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ComparablePredicatesReason))
		Expect(ready.Message).To(ContainSubstring(mirrorTransformer + " (broader)"))
		Expect(ready.Message).To(ContainSubstring(deployTransformer + " (narrower)"))
		Expect(ready.Message).To(ContainSubstring(containerResource))
	})

	It("reports both findings under the routing reason when the platform is neither routable nor discriminated", func() {
		r, _ := failureReconciler(platformstore.NewStore())
		plat := createSingleton()

		refuse(r, plat, both())

		ready := readyCondition(fetchPlatform())
		Expect(ready.Reason).To(Equal(status.OverSubscribedContractsReason),
			"over-subscription is the reported reason when both refusals hold")
		Expect(ready.Message).To(ContainSubstring("platform is not routable"))
		Expect(ready.Message).To(ContainSubstring("platform is not discriminated"))
	})

	It("leaves the store, the package identity, the registry and the prior report untouched", func() {
		store := platformstore.NewStore()
		lastGood := generatedMarker(7)
		store.SetGenerated(lastGood)

		r, _ := failureReconciler(store)
		plat := createSingleton()
		seedLastGoodStatus(plat)

		refuse(r, plat, overSubscribed())

		held, ok := store.Generated()
		Expect(ok).To(BeTrue(), "the last good package must keep serving renders through a refusal")
		Expect(held.Platform).To(BeIdenticalTo(lastGood.Platform))
		Expect(store.Identity()).To(Equal(platformIdentity(7)))

		fetched := fetchPlatform()
		Expect(fetched.Status.PackageIdentity).To(Equal(platformIdentity(7).String()),
			"status must keep describing the package renders consume")
		Expect(fetched.Status.Registry).To(Equal(lastGoodRegistry))

		report := apimeta.FindStatusCondition(fetched.Status.Conditions, status.ContractsFulfilledCondition)
		Expect(report).NotTo(BeNil(), "a refusal must not clear the report on the last good package")
		Expect(report.Reason).To(Equal(status.NoContractsDefinedReason))
		Expect(report.Message).To(Equal(priorReport))
	})

	It("sets observedGeneration, emits one warning event and requeues on the stalled interval", func() {
		r, recorder := failureReconciler(platformstore.NewStore())
		plat := createSingleton()

		res := refuse(r, plat, overSubscribed())
		Expect(res.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval),
			"a refusal is not transient: the fix is a Platform edit or a claim change")

		fetched := fetchPlatform()
		Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
		Expect(fetched.Status.ObservedGeneration).NotTo(BeZero())
		Expect(recorder.Events).To(HaveLen(1))
	})

	It("holds nothing when the very first generation is refused", func() {
		store := platformstore.NewStore()
		r, _ := failureReconciler(store)
		plat := createSingleton()

		refuse(r, plat, overSubscribed())

		_, ok := store.Generated()
		Expect(ok).To(BeFalse(),
			"a first-boot refusal holds no package: instances wait at PlatformNotReady with the cause on the Platform")
		Expect(store.Identity().IsZero()).To(BeTrue())
		Expect(readyCondition(fetchPlatform()).Reason).To(Equal(status.OverSubscribedContractsReason))
	})

	It("does not re-emit the warning event when the same verdict recurs", func() {
		r, recorder := failureReconciler(platformstore.NewStore())
		plat := createSingleton()

		// Two builds of one broken platform, reported in opposite
		// comprehension orders: the sorted message is the same, so the
		// stalled recheck must stay quiet.
		refuse(r, plat, overSubscribed())

		reordered := overSubscribed()
		reordered.RequiredBy[backupTrait] = []string{k8upSchedule, veleroSchedule}
		refuse(r, plat, reordered)

		Expect(recorder.Events).To(HaveLen(1),
			"an unchanged refusal must not re-emit the warning event on recheck")
	})
})
