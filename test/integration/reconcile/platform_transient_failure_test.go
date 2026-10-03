/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile_test

import (
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/kernel"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// The registry refuses the catalog's module file once, then serves it. The
// same reconciler, with no field touched, no Platform edit and no process
// restart, must go from BuildFailed to Ready. CUE's module cache keeps a
// lookup error in memory for the life of its source, so this holds only
// because each reconcile builds a module-file source of its own.
var _ = Describe("Platform transient registry failure (registry-backed)", func() {
	It("recovers from a transient registry failure on the same reconciler", func() {
		_, liveRegistry, catalogPath := liveBuildKernelOrSkip()
		front := newFlakyFront(liveRegistry, catalogPath)
		basePath, _, _ := strings.Cut(catalogPath, "@")

		plat, generation := createClusterPlatform(catalogPath)

		// The operator's shape: one mapping for the Kernel and the reconciler.
		store := platformstore.NewStore()
		r := &opmcontroller.PlatformReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: events.NewFakeRecorder(10),
			Kernel:        kernel.New(kernel.WithRegistry(front.Registry)),
			Store:         store,
			Registry:      front.Registry,
			Layout:        platformstore.Layout{Root: filepath.Join(GinkgoT().TempDir(), "platform")},
		}
		req := ctrl.Request{NamespacedName: client.ObjectKey{Name: recoveryPlatformName}}

		// The cache stays empty through both phases, so phase 2 is a real
		// registry round trip, not a disk hit.
		useEmptyCUECache()

		// Phase 1: the front refuses → BuildFailed, requeued, store empty.
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(front.Refused()).To(BeNumerically(">", 0), "the catalog lookup must have reached the front")

		failed := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), failed)).To(Succeed())
		ready := apimeta.FindStatusCondition(failed.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.BuildFailedReason))
		Expect(ready.Message).To(ContainSubstring(basePath))
		Expect(ready.Message).To(ContainSubstring("503"), "phase 1 must fail on the front's refusal")
		_, held := store.Generated()
		Expect(held).To(BeFalse())

		// Phase 2: the registry serves again. Nothing else changes.
		front.Forward()

		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		recovered := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), recovered)).To(Succeed())
		ready = apimeta.FindStatusCondition(recovered.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue),
			"a cached lookup error must not outlive its reconcile: %s", ready.Message)
		Expect(ready.Reason).To(Equal(status.GeneratedReason))
		Expect(res.RequeueAfter).To(BeZero())
		Expect(recovered.Generation).To(Equal(generation))
		Expect(recovered.Status.ObservedGeneration).To(Equal(generation))
		Expect(front.Forwarded()).To(BeNumerically(">", 0),
			"the catalog's module file must come back from the registry, not the disk")

		got, held := store.Generated()
		Expect(held).To(BeTrue())
		Expect(got.Identity).To(Equal(platformstore.NewPackageIdentity(generation, nil)))
	})
})
