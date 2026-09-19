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
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/helper/platformmodule"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// claimCatalogPath is the catalog a platform spec's active claim contributes:
// a real, resolvable one the spec's own registry does not subscribe, so the
// generated module has to pin and import it because the claim said so.
const claimCatalogPath = "opmodel.dev/catalogs/k8s@v1"

// claimCatalogVersion is the build claimCatalogPath is claimed at.
const claimCatalogVersion = "1.0.0-alpha.2"

// storeActiveClaim stores a claim already accepted and active — the verdict
// the claim reconciler writes and this capability only consumes. The platform
// specs never run acceptance: judging a claim is not this reconciler's job
// (the claim reconciler stays the judge).
func storeActiveClaim(name, catalogPath, version string) *releasesv1alpha1.TransformerRegistration {
	GinkgoHelper()
	claim := &releasesv1alpha1.TransformerRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: releasesv1alpha1.TransformerRegistrationSpec{
			Catalog:  catalogPath,
			Version:  version,
			Provides: []string{},
			ProviderRef: releasesv1alpha1.ProviderReference{
				Namespace: "default",
				Name:      providerInstanceName,
			},
		},
	}
	Expect(k8sClient.Create(ctx, claim)).To(Succeed())
	claim.Status.Accepted = true
	claim.Status.Active = true
	Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())
	return claim
}

// deleteAllClaims removes every TransformerRegistration and waits for the
// cache to agree. The kind is cluster-scoped and the acceptance specs
// deliberately leave active claims behind, so a platform spec — whose
// generated package is now a function of the active set — has to start from a
// known one.
//
// The guard finalizer is stripped first. An accepted claim carries it, and
// nothing in this suite runs the claim reconciler that would release it, so a
// plain delete would leave every such claim terminating forever and this wait
// would never finish. Stripping it here is the suite standing in for the
// reaper, not a statement about what the guard should do.
func deleteAllClaims() {
	GinkgoHelper()
	Expect(k8sClient.DeleteAllOf(ctx, &releasesv1alpha1.TransformerRegistration{})).To(Succeed())

	var terminating releasesv1alpha1.TransformerRegistrationList
	Expect(k8sClient.List(ctx, &terminating)).To(Succeed())
	for i := range terminating.Items {
		claim := &terminating.Items[i]
		if !controllerutil.ContainsFinalizer(claim, ClaimFinalizerName) {
			continue
		}
		mergePatch := client.MergeFrom(claim.DeepCopy())
		controllerutil.RemoveFinalizer(claim, ClaimFinalizerName)
		Expect(client.IgnoreNotFound(k8sClient.Patch(ctx, claim, mergePatch))).To(Succeed())
	}

	Eventually(func(g Gomega) {
		var list releasesv1alpha1.TransformerRegistrationList
		g.Expect(k8sClient.List(ctx, &list)).To(Succeed())
		g.Expect(list.Items).To(BeEmpty())
	}).Should(Succeed())
}

// markPackage drops a sentinel file inside a generated package directory.
// Layout.Write stages and renames, so a regeneration replaces the directory
// and the sentinel with it: the sentinel surviving is proof that no
// regeneration happened, which is how the burst spec counts them.
func markPackage(dir string) {
	GinkgoHelper()
	Expect(os.WriteFile(filepath.Join(dir, ".not-regenerated"), []byte("x"), 0o600)).To(Succeed())
}

// packageWasRegenerated reports whether the directory has been rewritten
// since markPackage stamped it.
func packageWasRegenerated(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".not-regenerated"))
	return os.IsNotExist(err)
}

// registryEntries reads the generated platform.cue back as text. The
// generator emits one #registry entry per subscription, so asserting on the
// file is asserting on what the render builds against.
func registryText(dir string) string {
	GinkgoHelper()
	return string(readModule(dir)[platformmodule.PlatformFileName])
}

var _ = Describe("Platform generation from the active-claim set", func() {
	BeforeEach(deleteAllClaims)
	AfterEach(func() {
		deleteAllClaims()
		deletePlatform()
	})

	Context("the tuple, without a registry", func() {
		It("folds every active claim into the entries beside the authored subscriptions", func() {
			plat := &releasesv1alpha1.Platform{
				Spec: releasesv1alpha1.PlatformSpec{
					Type: "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{
						"opmodel.dev/catalogs/authored@v1": {Version: "2.0.0"},
					},
				},
			}
			claims := []releasesv1alpha1.TransformerRegistration{
				claimSpec("opmodel.dev/catalogs/zebra@v1", "3.1.0"),
				claimSpec("opmodel.dev/catalogs/alpha@v2", "0.9.0"),
			}

			entries, err := platformEntries(plat, claims)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(Equal([]platformmodule.Entry{
				{Path: "opmodel.dev/catalogs/alpha@v2", Version: "0.9.0", Enable: true},
				{Path: "opmodel.dev/catalogs/authored@v1", Version: "2.0.0", Enable: true},
				{Path: "opmodel.dev/catalogs/zebra@v1", Version: "3.1.0", Enable: true},
			}), "claims are enabled entries, sorted in with the authored ones")
		})

		It("lets an authored subscription win over a claim naming the same catalog", func() {
			disabled := false
			plat := &releasesv1alpha1.Platform{
				Spec: releasesv1alpha1.PlatformSpec{
					Type: "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{
						"opmodel.dev/catalogs/shared@v1": {Version: "2.0.0", Enable: &disabled},
					},
				},
			}
			entries, err := platformEntries(plat, []releasesv1alpha1.TransformerRegistration{
				claimSpec("opmodel.dev/catalogs/shared@v1", "9.9.9"),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(Equal([]platformmodule.Entry{
				{Path: "opmodel.dev/catalogs/shared@v1", Version: "2.0.0", Enable: false},
			}), "a catalog is one #registry key, and the admin's pin and enable decision hold")
		})

		It("produces the spec-alone entries when no claim is active", func() {
			plat := &releasesv1alpha1.Platform{
				Spec: releasesv1alpha1.PlatformSpec{
					Type: "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{
						"opmodel.dev/catalogs/authored@v1": {Version: "2.0.0"},
					},
				},
			}
			entries, err := platformEntries(plat, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(Equal([]platformmodule.Entry{
				{Path: "opmodel.dev/catalogs/authored@v1", Version: "2.0.0", Enable: true},
			}))
			Expect(platformstore.NewPackageIdentity(4, nil).String()).To(Equal("gen-4"),
				"a claimless platform keeps the identity — and so the directory — it had before claims existed")
		})

		It("reads the current state, so any number of claims collapses into one tuple", func() {
			claims := make([]releasesv1alpha1.TransformerRegistration, 0, 5)
			for _, name := range []string{"e", "d", "c", "b", "a"} {
				claims = append(claims, claimSpec("opmodel.dev/catalogs/"+name+"@v1", "1.0.0"))
			}
			identity := platformstore.NewPackageIdentity(2, claimCoordinates(claims))
			Expect(identity.Claims()).To(HaveLen(5))

			// The same five claims arriving in any order are the same tuple:
			// nothing about the waking event survives into the identity.
			reversed := make([]releasesv1alpha1.TransformerRegistration, 0, 5)
			for i := len(claims) - 1; i >= 0; i-- {
				reversed = append(reversed, claims[i])
			}
			Expect(platformstore.NewPackageIdentity(2, claimCoordinates(reversed))).To(Equal(identity))
		})
	})

	Context("the claim watch", func() {
		It("enqueues the singleton whatever claim woke it", func() {
			requests := mapClaimToPlatform(ctx, &releasesv1alpha1.TransformerRegistration{
				ObjectMeta: metav1.ObjectMeta{Name: "somewhere.else"},
			})
			Expect(requests).To(Equal([]ctrl.Request{
				{NamespacedName: client.ObjectKey{Name: platformSingletonName}},
			}))
		})

		It("passes only the events that can move the active set", func() {
			p := claimContributionPredicate()
			unjudged := claimObject("opmodel.dev/catalogs/k8up@v1", "1.0.0", false, false)
			accepted := claimObject("opmodel.dev/catalogs/k8up@v1", "1.0.0", true, false)
			active := claimObject("opmodel.dev/catalogs/k8up@v1", "1.0.0", true, true)
			repinned := claimObject("opmodel.dev/catalogs/k8up@v1", "1.1.0", true, true)
			recatalogued := claimObject("opmodel.dev/catalogs/velero@v1", "1.0.0", true, true)

			Expect(p.Create(event.CreateEvent{Object: unjudged})).To(BeFalse(),
				"an un-judged claim contributes nothing, so it wakes nothing")
			Expect(p.Create(event.CreateEvent{Object: active})).To(BeTrue(),
				"an already-active claim resurfacing must be folded in")
			Expect(p.Delete(event.DeleteEvent{Object: active})).To(BeTrue())
			Expect(p.Delete(event.DeleteEvent{Object: accepted})).To(BeFalse())

			Expect(p.Update(event.UpdateEvent{ObjectOld: accepted, ObjectNew: active})).To(BeTrue(),
				"activation is the event this capability exists for")
			Expect(p.Update(event.UpdateEvent{ObjectOld: active, ObjectNew: accepted})).To(BeTrue(),
				"deactivation shrinks the set just as visibly")
			Expect(p.Update(event.UpdateEvent{ObjectOld: active, ObjectNew: repinned})).To(BeTrue(),
				"a claim repointed at another build is a different registry")
			Expect(p.Update(event.UpdateEvent{ObjectOld: active, ObjectNew: recatalogued})).To(BeTrue(),
				"a claim repointed at another catalog is a different registry too")
			Expect(p.Update(event.UpdateEvent{ObjectOld: active, ObjectNew: active})).To(BeFalse(),
				"a resync of an unchanged claim computes the identity already held")
			Expect(p.Update(event.UpdateEvent{ObjectOld: unjudged, ObjectNew: accepted})).To(BeFalse(),
				"acceptance alone does not put a catalog in the registry")
		})
	})

	Context("generate and build (requires a reachable registry)", func() {
		It("regenerates on a claim activating, without the Platform CR being edited", func() {
			k, reg := buildKernelOrSkip()
			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{testCatalogPath(): {Version: fixtures.CatalogVersion()}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			before, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(before.Identity.Claims()).To(BeEmpty())
			Expect(registryText(before.Dir)).NotTo(ContainSubstring(claimCatalogPath))

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
			generationBefore := plat.Generation

			storeActiveClaim("default."+providerInstanceName, claimCatalogPath, claimCatalogVersion)
			Eventually(func(g Gomega) {
				var list releasesv1alpha1.TransformerRegistrationList
				g.Expect(k8sClient.List(ctx, &list)).To(Succeed())
				g.Expect(list.Items).To(HaveLen(1))
			}).Should(Succeed())

			_, err = r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())

			fetched := &releasesv1alpha1.Platform{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
			Expect(fetched.Generation).To(Equal(generationBefore),
				"the claim reached the platform without the CR being edited")
			Expect(readyCondition(fetched).Reason).To(Equal(status.GeneratedReason))

			after, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(after.Identity).NotTo(Equal(before.Identity),
				"a claim activating yields a new identity at an unchanged generation")
			Expect(after.Identity.Generation()).To(Equal(generationBefore))
			Expect(after.Identity.Claims()).To(Equal([]string{claimCatalogPath + "@" + claimCatalogVersion}))
			Expect(after.Dir).NotTo(Equal(before.Dir))

			Expect(registryText(after.Dir)).To(ContainSubstring(claimCatalogPath),
				"the active provider's catalog is a registry entry")
			Expect(registryText(after.Dir)).To(ContainSubstring(claimCatalogVersion),
				"pinned at the version the claim named")

			// Status is the Platform's own account of what the render builds
			// against: the identity, and the union with each entry's source.
			Expect(fetched.Status.PackageIdentity).To(Equal(after.Identity.String()))
			Expect(fetched.Status.Registry).To(ConsistOf(
				releasesv1alpha1.ResolvedRegistryEntry{
					Catalog: testCatalogPath(),
					Version: fixtures.CatalogVersion(),
					Enabled: true,
					Source:  releasesv1alpha1.RegistryEntrySubscription,
				},
				releasesv1alpha1.ResolvedRegistryEntry{
					Catalog: claimCatalogPath,
					Version: claimCatalogVersion,
					Enabled: true,
					Source:  releasesv1alpha1.RegistryEntryRegistration,
				},
			), "the union lists both sources, distinguishing which is which")
		})

		It("follows the active set in both directions on status.registry", func() {
			k, reg := buildKernelOrSkip()
			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{testCatalogPath(): {Version: fixtures.CatalogVersion()}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			claim := storeActiveClaim("default."+providerInstanceName, claimCatalogPath, claimCatalogVersion)
			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(catalogsInUnion(fetchPlatform())).To(ContainElement(claimCatalogPath))
			identityWithClaim := fetchPlatform().Status.PackageIdentity

			// The claim stops being active. The union must shed its catalog.
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(claim), claim)).To(Succeed())
			claim.Status.Active = false
			Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())
			Eventually(func(g Gomega) {
				var stored releasesv1alpha1.TransformerRegistration
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(claim), &stored)).To(Succeed())
				g.Expect(stored.Status.Active).To(BeFalse())
			}).Should(Succeed())

			_, err = r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			shrunk := fetchPlatform()
			Expect(catalogsInUnion(shrunk)).NotTo(ContainElement(claimCatalogPath),
				"the union no longer lists a catalog no claim contributes")
			Expect(catalogsInUnion(shrunk)).To(ConsistOf(testCatalogPath()))
			Expect(shrunk.Status.PackageIdentity).NotTo(Equal(identityWithClaim))
			Expect(shrunk.Status.PackageIdentity).To(Equal(platformIdentity(shrunk.Generation).String()))
		})

		It("leaves the identity and the union describing the last-good package when a build fails", func() {
			k, reg := buildKernelOrSkip()
			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{testCatalogPath(): {Version: fixtures.CatalogVersion()}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			good := fetchPlatform()
			Expect(good.Status.PackageIdentity).NotTo(BeEmpty())
			Expect(catalogsInUnion(good)).To(ConsistOf(testCatalogPath()))

			// A claim naming a catalog that does not resolve: the tuple moves,
			// the build fails, and the last-good package is still the one the
			// store holds and every render is consuming.
			storeActiveClaim("default."+providerInstanceName, "opmodel.dev/catalogs/does-not-exist@v1", "9.9.9")
			Eventually(func(g Gomega) {
				var list releasesv1alpha1.TransformerRegistrationList
				g.Expect(k8sClient.List(ctx, &list)).To(Succeed())
				g.Expect(list.Items).To(HaveLen(1))
			}).Should(Succeed())

			res, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).NotTo(BeZero(), "a failed build requeues")

			failed := fetchPlatform()
			Expect(readyCondition(failed).Reason).To(Equal(status.BuildFailedReason))
			Expect(failed.Status.PackageIdentity).To(Equal(good.Status.PackageIdentity),
				"a failed generation must not advertise an identity nothing built")
			Expect(catalogsInUnion(failed)).To(ConsistOf(testCatalogPath()),
				"the union keeps describing the package the store still holds")

			held, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(held.Identity.String()).To(Equal(good.Status.PackageIdentity),
				"status and the held package agree, which is what the identity is for")
		})

		It("computes from current state, so a stale event regenerates what the state implies", func() {
			k, reg := buildKernelOrSkip()
			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{testCatalogPath(): {Version: fixtures.CatalogVersion()}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			claim := storeActiveClaim("default."+providerInstanceName, claimCatalogPath, claimCatalogVersion)
			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			withClaim, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(withClaim.Identity.Claims()).To(HaveLen(1))

			// The claim goes away. The next wake-up is, as far as the
			// reconciler can tell, the very same event as the one that folded
			// the claim in: the handler discards the object either way.
			Expect(k8sClient.Delete(ctx, claim)).To(Succeed())
			Eventually(func(g Gomega) {
				var list releasesv1alpha1.TransformerRegistrationList
				g.Expect(k8sClient.List(ctx, &list)).To(Succeed())
				g.Expect(list.Items).To(BeEmpty())
			}).Should(Succeed())

			_, err = r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			withoutClaim, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(withoutClaim.Identity.Claims()).To(BeEmpty(),
				"the package follows current state, never the event's content")
			Expect(registryText(withoutClaim.Dir)).NotTo(ContainSubstring(claimCatalogPath))
		})

		It("converges a burst of activations in one regeneration, covering every claim", func() {
			k, reg := buildKernelOrSkip()
			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			// Both claims flip active together, as a batch of provider
			// installs completing does.
			claimed := map[string]string{
				testCatalogPath(): fixtures.CatalogVersion(),
				claimCatalogPath:  claimCatalogVersion,
			}
			names := []string{"default." + providerInstanceName, "burst." + providerInstanceName}
			i := 0
			for path, version := range claimed {
				storeActiveClaim(names[i], path, version)
				i++
			}
			Eventually(func(g Gomega) {
				var list releasesv1alpha1.TransformerRegistrationList
				g.Expect(k8sClient.List(ctx, &list)).To(Succeed())
				g.Expect(list.Items).To(HaveLen(len(claimed)))
			}).Should(Succeed())

			// One wake-up per claim, plus spares: level-computation means the
			// first covers every claim and the rest find the identity they
			// already hold.
			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			rec, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(rec.Identity.Claims()).To(HaveLen(len(claimed)),
				"the first regeneration already reflects every claim in the burst")
			markPackage(rec.Dir)

			for range 4 {
				_, err := r.Reconcile(ctx, clusterRequest)
				Expect(err).NotTo(HaveOccurred())
			}

			after, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(after.Identity).To(Equal(rec.Identity))
			Expect(after.Dir).To(Equal(rec.Dir))
			Expect(packageWasRegenerated(rec.Dir)).To(BeFalse(),
				"five wake-ups over one active set must regenerate exactly once")

			text := registryText(rec.Dir)
			for path := range claimed {
				Expect(text).To(ContainSubstring(path), "the final package reflects every claim")
			}
			Expect(readyCondition(fetchPlatform()).Reason).To(Equal(status.GeneratedReason),
				"a skipped regeneration still reports the platform ready")
		})
	})
})

// claimSpec is a claim carrying only what the tuple reads: no status, since
// platformEntries is given the already-filtered active set.
func claimSpec(catalogPath, version string) releasesv1alpha1.TransformerRegistration {
	return releasesv1alpha1.TransformerRegistration{
		Spec: releasesv1alpha1.TransformerRegistrationSpec{Catalog: catalogPath, Version: version},
	}
}

// claimObject is a claim as the watch predicate sees one: spec coordinates
// plus the claim reconciler's verdict.
func claimObject(catalogPath, version string, accepted, active bool) *releasesv1alpha1.TransformerRegistration {
	claim := claimSpec(catalogPath, version)
	claim.Status.Accepted = accepted
	claim.Status.Active = active
	return &claim
}

// catalogsInUnion lists the catalogs status.registry reports.
func catalogsInUnion(plat *releasesv1alpha1.Platform) []string {
	out := make([]string, 0, len(plat.Status.Registry))
	for _, entry := range plat.Status.Registry {
		out = append(out, entry.Catalog)
	}
	return out
}

// fetchPlatform reads the singleton back.
func fetchPlatform() *releasesv1alpha1.Platform {
	GinkgoHelper()
	fetched := &releasesv1alpha1.Platform{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: platformSingletonName}, fetched)).To(Succeed())
	return fetched
}
