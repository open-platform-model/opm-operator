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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/internal/version"
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

// registrySkip skips the current spec for a missing registry prerequisite —
// unless OPM_TEST_REGISTRY_FORCE=1 (set in CI), in which case the missing
// prerequisite is a hard failure so registry-seam coverage cannot evaporate
// silently in an environment that promises to provide the registry. Mirrors
// the library's OPM_FLOW_TEST_FORCE idiom.
func registrySkip(msg string) {
	if os.Getenv("OPM_TEST_REGISTRY_FORCE") == "1" {
		Fail("OPM_TEST_REGISTRY_FORCE=1 but registry prerequisite missing: " + msg)
	}
	Skip(msg)
}

// testCatalogVersion is the exact catalog build the registry-backed specs
// subscribe to (enhancement 0010 D14: a subscription names one published
// build; there is no range vocabulary). Overridable via
// OPM_TEST_CATALOG_VERSION so a fixture republish does not require a code
// edit; the default tracks the pin in config/samples.
func testCatalogVersion() string {
	if v := os.Getenv("OPM_TEST_CATALOG_VERSION"); v != "" {
		return v
	}
	return "2.0.0-alpha.5"
}

// materializeKernelOrSkip builds a Kernel from CUE_REGISTRY and skips the spec
// unless it can synthesize+materialize a trivial (no-subscription) platform —
// i.e. the registry is reachable and a matching opmodel.dev/core schema is
// resolvable. Materialize itself requires registry I/O; both the ghcr mapping
// (`task dev:test`, CI) and a local registry with core published
// (`task dev:test:local`) satisfy it.
func materializeKernelOrSkip() *kernel.Kernel {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		registrySkip("CUE_REGISTRY not set — platform materialize specs need a reachable registry serving opmodel.dev/core")
	}
	k := kernel.New(kernel.WithRegistry(reg))
	probe, err := k.SynthesizePlatform(ctx, synth.PlatformInput{Name: platformSingletonName, Type: "kubernetes"})
	if err != nil {
		registrySkip("opmodel.dev/core schema not resolvable from CUE_REGISTRY: " + err.Error())
	}
	if _, err := k.Materialize(ctx, probe); err != nil {
		registrySkip("trivial platform did not materialize from CUE_REGISTRY: " + err.Error())
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
				registrySkip("OPM_TEST_CATALOG_PATH not set — no resolvable catalog subscription fixture available")
			}

			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{catalogPath: {Version: testCatalogVersion()}},
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
			Expect(fetched.Status.OperatorVersion).To(Equal(version.Full()))

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
					Registry: map[string]releasesv1alpha1.Subscription{bogus: {Version: "9.9.9"}},
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

	Context("stored legacy singleton (versionless subscription)", func() {
		It("surfaces the missing version as MaterializeFailed naming the subscription path", func() {
			// A versionless subscription cannot be created through admission —
			// version is CRD-required — so it exists only as a stored object
			// predating the filter→version reshape, which validation
			// ratcheting keeps status-patchable. Seed a fake client with that
			// stored shape; the library refuses it at synthesis
			// (synth.ErrSubscriptionMissingVersion) before any registry I/O,
			// so the Kernel needs no reachable registry.
			const legacyPath = "opmodel.dev/catalogs/opm"
			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName, Generation: 3},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{legacyPath: {}},
				},
			}
			c := fake.NewClientBuilder().
				WithScheme(k8sClient.Scheme()).
				WithStatusSubresource(&releasesv1alpha1.Platform{}).
				WithObjects(plat).
				Build()

			store := platformstore.NewStore()
			r := &PlatformReconciler{
				Client:        c,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Kernel:        kernel.New(),
				Store:         store,
			}

			res, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0), "a stalled Platform rechecks on a bounded interval")

			fetched := &releasesv1alpha1.Platform{}
			Expect(c.Get(ctx, client.ObjectKey{Name: platformSingletonName}, fetched)).To(Succeed())
			ready := apimeta.FindStatusCondition(fetched.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.MaterializeFailedReason))
			Expect(ready.Message).To(ContainSubstring(legacyPath), "message should name the versionless subscription")
			Expect(ready.Message).To(ContainSubstring("Version is required"))
			Expect(fetched.Status.ObservedGeneration).To(Equal(plat.Generation))

			_, held := store.Get()
			Expect(held).To(BeFalse(), "nothing must materialize for a versionless subscription")
		})
	})
})
