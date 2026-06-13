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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/materialize"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// clusterRequest is the reconcile request for the singleton Platform.
var clusterRequest = ctrl.Request{NamespacedName: client.ObjectKey{Name: platformSingletonName}}

// materializeMarker returns a fresh, distinct *MaterializedPlatform usable as a
// store sentinel for identity (BeIdenticalTo) assertions.
func materializeMarker() *materialize.MaterializedPlatform {
	return &materialize.MaterializedPlatform{}
}

// newPlatformReconciler builds a PlatformReconciler over the given store with a
// fake event recorder and the supplied kernel (may be nil for paths that never
// reach synthesis/materialize).
func newPlatformReconciler(store *platformstore.Store, k *kernel.Kernel) *PlatformReconciler {
	return &PlatformReconciler{
		Client:        k8sClient,
		Scheme:        k8sClient.Scheme(),
		EventRecorder: events.NewFakeRecorder(10),
		Kernel:        k,
		Store:         store,
	}
}

// materializeKernelOrSkip builds a Kernel from CUE_REGISTRY and skips the spec
// unless it can synthesize+materialize a trivial (no-subscription) platform —
// i.e. the registry is reachable and the matching opmodel.dev/core@v0 schema is
// resolvable. Materialize itself requires registry I/O, so these specs cannot
// run in the default CI path (ghcr lacks the local fixtures); run them with a
// local registry that has core@v0 published.
func materializeKernelOrSkip() *kernel.Kernel {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		Skip("CUE_REGISTRY not set — platform materialize specs need a reachable registry with opmodel.dev/core@v0")
	}
	k := kernel.New(kernel.WithRegistry(reg))
	probe, err := k.SynthesizePlatform(ctx, synth.PlatformInput{Name: platformSingletonName, Type: "kubernetes"})
	if err != nil {
		Skip("opmodel.dev/core schema not resolvable from CUE_REGISTRY: " + err.Error())
	}
	if _, err := k.Materialize(ctx, probe); err != nil {
		Skip("trivial platform did not materialize from CUE_REGISTRY: " + err.Error())
	}
	return k
}

func deletePlatform(name string) {
	plat := &releasesv1alpha1.Platform{ObjectMeta: metav1.ObjectMeta{Name: name}}
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, plat))).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, &releasesv1alpha1.Platform{})
		return client.IgnoreNotFound(err) == nil && err != nil
	}).Should(BeTrue(), "Platform %q should be fully deleted", name)
}

var _ = Describe("Platform Controller", func() {
	AfterEach(func() {
		deletePlatform(platformSingletonName)
	})

	Context("singleton guard", func() {
		It("ignores a reconcile request for a non-cluster name without touching the store", func() {
			store := platformstore.NewStore()
			held := materializeMarker()
			store.Set(5, held)

			r := newPlatformReconciler(store, nil)
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: "not-cluster"}})
			Expect(err).NotTo(HaveOccurred())

			got, ok := store.Get()
			Expect(ok).To(BeTrue(), "non-cluster reconcile must not clear the store")
			Expect(got).To(BeIdenticalTo(held))
		})
	})

	Context("deletion", func() {
		It("clears the store when the cluster Platform is absent", func() {
			store := platformstore.NewStore()
			store.Set(3, materializeMarker())

			r := newPlatformReconciler(store, nil)
			// No cluster Platform exists → Get returns NotFound → store cleared.
			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())

			_, ok := store.Get()
			Expect(ok).To(BeFalse(), "store should report no held platform after the Platform is gone")
			Expect(store.Generation()).To(BeZero())
		})
	})

	Context("materialize (requires a reachable registry)", func() {
		It("materializes a resolvable platform: Ready=True/Materialized, observedGeneration set, store populated", func() {
			k := materializeKernelOrSkip()
			catalogPath := os.Getenv("OPM_TEST_CATALOG_PATH")
			if catalogPath == "" {
				Skip("OPM_TEST_CATALOG_PATH not set — no resolvable catalog subscription fixture available")
			}

			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{catalogPath: {}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())

			fetched := &releasesv1alpha1.Platform{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
			ready := apimeta.FindStatusCondition(fetched.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).To(Equal(status.MaterializedReason))
			Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))

			held, ok := store.Get()
			Expect(ok).To(BeTrue(), "store should hold the materialized platform")
			Expect(held).NotTo(BeNil())
			Expect(store.Generation()).To(Equal(fetched.Generation))
		})

		It("surfaces a MaterializeError as Ready=False/MaterializeFailed and leaves the store unchanged", func() {
			k := materializeKernelOrSkip()

			store := platformstore.NewStore()
			lastGood := materializeMarker()
			store.Set(1, lastGood) // pre-existing good platform must survive a failure

			r := newPlatformReconciler(store, k)

			const bogus = "testing.opmodel.dev/catalogs/does-not-exist"
			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{bogus: {}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())

			fetched := &releasesv1alpha1.Platform{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
			ready := apimeta.FindStatusCondition(fetched.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.MaterializeFailedReason))
			Expect(ready.Message).To(ContainSubstring(bogus), "message should name the failing subscription")

			// Last-good platform is preserved on failure (§8.4 freeze posture).
			held, ok := store.Get()
			Expect(ok).To(BeTrue())
			Expect(held).To(BeIdenticalTo(lastGood))
			Expect(store.Generation()).To(Equal(int64(1)))
		})
	})
})
