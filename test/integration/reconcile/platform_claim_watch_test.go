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
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/kernel"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// This is the end-to-end counterpart to the unit-level claim-watch specs
// (mapClaimToPlatform and claimContributionPredicate in internal/controller):
// it runs a real manager so PlatformReconciler.SetupWithManager's
// TransformerRegistration watch actually fires, proving a claim activating
// regenerates the platform on its own rather than waiting for an unrelated
// reconcile or an edit to the Platform CR (0015:D13).
//
// It is registry-backed: the regenerated package has to pin, import and build
// the claim's catalog, which is the half a fake store cannot exercise.
var _ = Describe("Claim-driven regeneration (manager-driven, registry-backed)", func() {
	const (
		platformName = "cluster"
		claimName    = "claim-watch-ns.provider"
		// A second real catalog the spec's Platform does not subscribe, so its
		// arrival in the generated module can only have come from the claim.
		claimCatalog        = "opmodel.dev/catalogs/k8s@v1"
		claimCatalogVersion = "1.0.0-alpha.2"
	)

	AfterEach(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.TransformerRegistration{
			ObjectMeta: metav1.ObjectMeta{Name: claimName},
		}))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: platformName},
		}))).To(Succeed())
	})

	It("regenerates when a claim becomes active, with no edit to the Platform CR", func() {
		skipIfNoTestRegistry()
		registry := os.Getenv("CUE_REGISTRY")

		mgrCtx, cancelMgr := context.WithCancel(ctx)
		defer cancelMgr()

		// Sibling specs run managers of their own; controller-runtime enforces
		// process-global controller-name uniqueness.
		skipNameValidation := true
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			LeaderElection:         false,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Controller:             config.Controller{SkipNameValidation: &skipNameValidation},
		})
		Expect(err).NotTo(HaveOccurred())

		store := platformstore.NewStore()
		reconciler := &opmcontroller.PlatformReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: events.NewFakeRecorder(32),
			Kernel:        kernel.New(kernel.WithRegistry(registry)),
			Store:         store,
			Registry:      registry,
			Layout:        platformstore.Layout{Root: filepath.Join(GinkgoT().TempDir(), "platform")},
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(mgrCtx)).To(Succeed())
		}()
		Expect(mgr.GetCache().WaitForCacheSync(mgrCtx)).To(BeTrue())

		plat := &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: platformName},
			Spec: releasesv1alpha1.PlatformSpec{
				Type:     "kubernetes",
				Registry: map[string]releasesv1alpha1.Subscription{testCatalogPath(): {Version: testCatalogVersion()}},
			},
		}
		Expect(k8sClient.Create(ctx, plat)).To(Succeed())

		// Phase 1: the Platform watch builds the claimless package.
		var claimless string
		Eventually(func(g Gomega) {
			var current releasesv1alpha1.Platform
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: platformName}, &current)).To(Succeed())
			ready := apimeta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue),
				"the platform must build before the claim is judged; reason=%s", ready.Reason)
			g.Expect(current.Status.PackageIdentity).NotTo(BeEmpty())
			claimless = current.Status.PackageIdentity
		}).WithTimeout(90 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: platformName}, plat)).To(Succeed())
		generationBefore := plat.Generation

		// Phase 2: a claim is stored un-judged. It contributes nothing, so the
		// predicate drops the create and nothing regenerates.
		claim := &releasesv1alpha1.TransformerRegistration{
			ObjectMeta: metav1.ObjectMeta{Name: claimName},
			Spec: releasesv1alpha1.TransformerRegistrationSpec{
				Catalog:  claimCatalog,
				Version:  claimCatalogVersion,
				Provides: []string{},
				ProviderRef: releasesv1alpha1.ProviderReference{
					Namespace: "default",
					Name:      "provider",
				},
			},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		Consistently(func(g Gomega) {
			var current releasesv1alpha1.Platform
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: platformName}, &current)).To(Succeed())
			g.Expect(current.Status.PackageIdentity).To(Equal(claimless))
		}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		// Phase 3: the claim reconciler's verdict lands. Nothing touches the
		// Platform CR; only the claim watch can carry this through.
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: claimName}, claim)).To(Succeed())
		claim.Status.Accepted = true
		claim.Status.Active = true
		Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())

		Eventually(func(g Gomega) {
			var current releasesv1alpha1.Platform
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: platformName}, &current)).To(Succeed())
			g.Expect(current.Status.PackageIdentity).NotTo(Equal(claimless),
				"activating a claim must regenerate under a new identity")
			ready := apimeta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s", ready.Reason)

			sources := map[string]releasesv1alpha1.RegistryEntrySource{}
			for _, entry := range current.Status.Registry {
				sources[entry.Catalog] = entry.Source
			}
			g.Expect(sources).To(HaveKeyWithValue(claimCatalog, releasesv1alpha1.RegistryEntryRegistration))
			g.Expect(sources).To(HaveKeyWithValue(testCatalogPath(), releasesv1alpha1.RegistryEntrySubscription))
			g.Expect(current.Generation).To(Equal(generationBefore),
				"the claim reached the platform without the CR being edited")
		}).WithTimeout(90 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		Expect(store.Identity().Claims()).To(Equal([]string{claimCatalog + "@" + claimCatalogVersion}),
			"the held package is the one the status identity names")
	})
})
