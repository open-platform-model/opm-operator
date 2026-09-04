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
	"maps"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
	"github.com/open-platform-model/library/opm/helper/platformmodule"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/platform"
	"github.com/open-platform-model/library/opm/schema"
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
	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// clusterRequest is the reconcile request for the singleton Platform.
var clusterRequest = ctrl.Request{NamespacedName: client.ObjectKey{Name: platformSingletonName}}

// generatedMarker returns a distinct generated-module record usable as a store
// sentinel for identity assertions.
func generatedMarker(gen int64) platformstore.Generated {
	return platformstore.Generated{Generation: gen, Dir: "/nonexistent/gen-marker", Platform: &platform.Platform{}}
}

// newPlatformReconciler builds a PlatformReconciler over the given store with a
// fake event recorder, the supplied kernel (may be nil for paths that never
// reach the build), the registry mapping and a temp platform directory.
func newPlatformReconciler(store *platformstore.Store, k *kernel.Kernel, registry string) *PlatformReconciler {
	return &PlatformReconciler{
		Client:        k8sClient,
		Scheme:        k8sClient.Scheme(),
		EventRecorder: events.NewFakeRecorder(10),
		Kernel:        k,
		Store:         store,
		Registry:      registry,
		Layout:        platformstore.Layout{Root: filepath.Join(GinkgoT().TempDir(), "platform")},
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

// buildKernelOrSkip builds a Kernel from CUE_REGISTRY and skips the spec
// unless the core the generated module pins is resolvable from it, i.e. the
// registry is reachable and serves opmodel.dev/core. Both the ghcr mapping
// (`task dev:test`, CI) and a seeded local registry satisfy it. Returns the
// kernel and the registry mapping.
func buildKernelOrSkip() (*kernel.Kernel, string) {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		registrySkip("CUE_REGISTRY not set — platform build specs need a reachable registry serving opmodel.dev/core")
	}
	src, err := newTestModFileSource(reg)
	Expect(err).NotTo(HaveOccurred())
	if _, err := platformmodule.Closure(ctx, src, platformmodule.Roots(nil)); err != nil {
		registrySkip("core " + schema.DefaultSchemaVersion() + " not resolvable from CUE_REGISTRY: " + err.Error())
	}
	return kernel.New(kernel.WithRegistry(reg)), reg
}

// newTestModFileSource is the module-file source the specs derive closures
// through: the reconciler's own configuration, minus the client type.
func newTestModFileSource(registry string) (platformmodule.ModFileSource, error) {
	return platformmodule.NewRegistry(platformmodule.RegistryConfig{Registry: registry, Env: os.Environ()})
}

// testCatalogPath is the catalog the registry-backed specs subscribe to:
// OPM_TEST_CATALOG_PATH when set (seeded CI), the first-party abstraction
// catalog otherwise (resolved from GHCR under `task dev:test`).
func testCatalogPath() string {
	if p := os.Getenv("OPM_TEST_CATALOG_PATH"); p != "" {
		return p
	}
	return "opmodel.dev/catalogs/opm@v4"
}

func deletePlatform(name string) {
	plat := &releasesv1alpha1.Platform{ObjectMeta: metav1.ObjectMeta{Name: name}}
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, plat))).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, &releasesv1alpha1.Platform{})
		return client.IgnoreNotFound(err) == nil && err != nil
	}).Should(BeTrue(), "Platform %q should be fully deleted", name)
}

func readyCondition(plat *releasesv1alpha1.Platform) *metav1.Condition {
	ready := apimeta.FindStatusCondition(plat.Status.Conditions, status.ReadyCondition)
	Expect(ready).NotTo(BeNil())
	return ready
}

func readModule(dir string) map[string][]byte {
	out := map[string][]byte{}
	for _, name := range []string{platformmodule.ModuleFileName, platformmodule.PlatformFileName} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		Expect(err).NotTo(HaveOccurred(), "generated module should carry %s", name)
		out[name] = data
	}
	return out
}

// requiringSource wraps a module-file source and adds one requirement to a
// given module's published module file: a generation defect model, where the
// derived closure pins a build the stamp disagrees with.
type requiringSource struct {
	platformmodule.ModFileSource
	target module.Version
	extra  module.Version
}

func (s requiringSource) ModFile(ctx context.Context, mv module.Version) (*modfile.File, error) {
	mf, err := s.ModFileSource.ModFile(ctx, mv)
	if err != nil || !mv.Equal(s.target) {
		return mf, err
	}
	deps := make(map[string]*modfile.Dep, len(mf.Deps)+1)
	maps.Copy(deps, mf.Deps)
	deps[s.extra.Path()] = &modfile.Dep{Version: s.extra.Version()}
	patched := &modfile.File{Module: mf.Module, Language: mf.Language, Deps: deps}
	if err := patched.Init(); err != nil {
		return nil, err
	}
	return patched, nil
}

var _ = Describe("Platform Controller", func() {
	AfterEach(func() {
		deletePlatform(platformSingletonName)
	})

	Context("singleton guard", func() {
		It("ignores a reconcile request for a non-cluster name without touching the store", func() {
			store := platformstore.NewStore()
			held := generatedMarker(5)
			store.SetGenerated(held)

			r := newPlatformReconciler(store, nil, "")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: "not-cluster"}})
			Expect(err).NotTo(HaveOccurred())

			got, ok := store.Generated()
			Expect(ok).To(BeTrue(), "non-cluster reconcile must not clear the store")
			Expect(got.Platform).To(BeIdenticalTo(held.Platform))
		})
	})

	Context("deletion", func() {
		It("clears the store when the cluster Platform is absent", func() {
			store := platformstore.NewStore()
			store.SetGenerated(generatedMarker(3))

			r := newPlatformReconciler(store, nil, "")
			// No cluster Platform exists → Get returns NotFound → store cleared.
			_, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())

			_, ok := store.Generated()
			Expect(ok).To(BeFalse(), "store should report no held platform after the Platform is gone")
			Expect(store.Generation()).To(BeZero())
		})
	})

	Context("generate and build (requires a reachable registry)", func() {
		It("generates and builds a clean CR: Ready=True/Generated, observedGeneration set, module recorded", func() {
			k, reg := buildKernelOrSkip()
			catalogPath := testCatalogPath()

			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)

			disabled := false
			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type: "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{
						catalogPath: {Version: fixtures.CatalogVersion()},
						// A disabled second entry keyed at an unpublished path: it must
						// still be pinned and imported, so it has to resolve.
						"opmodel.dev/catalogs/k8s@v1": {Version: "1.0.0-alpha.2", Enable: &disabled},
					},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			res, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeZero(), "a successful build does not requeue")

			fetched := &releasesv1alpha1.Platform{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
			ready := readyCondition(fetched)
			Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
			Expect(ready.Reason).To(Equal(status.GeneratedReason))
			Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
			Expect(fetched.Status.OperatorVersion).To(Equal(version.Full()))

			rec, ok := store.Generated()
			Expect(ok).To(BeTrue(), "store should hold the generated platform")
			Expect(rec.Platform).NotTo(BeNil())
			Expect(rec.Generation).To(Equal(fetched.Generation))
			Expect(rec.Dir).To(Equal(r.Layout.Dir(fetched.Generation)))
			Expect(store.Generation()).To(Equal(fetched.Generation))

			files := readModule(rec.Dir)
			mf, err := modfile.Parse(files[platformmodule.ModuleFileName], platformmodule.ModuleFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(mf.QualifiedModule()).To(Equal(PlatformModulePath))
			Expect(mf.Deps).To(HaveKey(catalogPath))
			Expect(mf.Deps[catalogPath].Version).To(Equal("v" + fixtures.CatalogVersion()))
			Expect(mf.Deps).To(HaveKey("opmodel.dev/catalogs/k8s@v1"))
			Expect(mf.Deps).To(HaveKey(platformmodule.CorePath))
			// Core pin follows the library: the generated module pins core at
			// the release the library verified its glue against, with no
			// operator-side constant involved.
			Expect(mf.Deps[platformmodule.CorePath].Version).To(Equal(schema.DefaultSchemaVersion()))
			Expect(string(files[platformmodule.PlatformFileName])).To(ContainSubstring("enable:   false"))

			// The record is what the render paths import the platform from:
			// it must carry its on-disk source and the default skew policy.
			Expect(rec.Platform.Source).NotTo(BeNil(), "the built platform must carry its source for Kernel.Render")
			Expect(rec.Platform.Source.Root).To(Equal(rec.Dir))
			Expect(rec.Skew).To(Equal(kernel.SkewWarn), "an unset spec.skewPolicy resolves to Warn")
		})

		It("regenerates byte-identical module content for the same generation", func() {
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
			first, ok := store.Generated()
			Expect(ok).To(BeTrue())
			before := readModule(first.Dir)

			_, err = r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			second, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(second.Generation).To(Equal(first.Generation))
			Expect(second.Dir).To(Equal(first.Dir))
			after := readModule(second.Dir)
			Expect(after).To(Equal(before), "the same generation must regenerate byte-identical content")

			gens, err := r.Layout.Generations()
			Expect(err).NotTo(HaveOccurred())
			Expect(gens).To(Equal([]int64{first.Generation}), "a same-generation rewrite leaves one directory")
		})

		It("prunes every superseded generation no render leases once the next generation builds", func() {
			k, reg := buildKernelOrSkip()
			catalogPath := testCatalogPath()
			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{catalogPath: {Version: fixtures.CatalogVersion()}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			// reconcileGeneration reconciles the singleton, asserts a clean build
			// and returns the generation the store now records.
			reconcileGeneration := func() int64 {
				GinkgoHelper()
				_, err := r.Reconcile(ctx, clusterRequest)
				Expect(err).NotTo(HaveOccurred())
				fetched := &releasesv1alpha1.Platform{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
				ready := readyCondition(fetched)
				Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
				rec, ok := store.Generated()
				Expect(ok).To(BeTrue())
				Expect(rec.Generation).To(Equal(fetched.Generation))
				return rec.Generation
			}
			// bumpSpec applies mutate to the stored spec; a spec change advances
			// metadata.generation, which is what the retention keys on.
			bumpSpec := func(mutate func(*releasesv1alpha1.PlatformSpec)) {
				GinkgoHelper()
				fetched := &releasesv1alpha1.Platform{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
				mutate(&fetched.Spec)
				Expect(k8sClient.Update(ctx, fetched)).To(Succeed())
			}

			gen1 := reconcileGeneration()

			disabled := false
			bumpSpec(func(s *releasesv1alpha1.PlatformSpec) {
				s.Registry["opmodel.dev/catalogs/k8s@v1"] = releasesv1alpha1.Subscription{Version: "1.0.0-alpha.2", Enable: &disabled}
			})
			gen2 := reconcileGeneration()
			Expect(gen2).To(BeNumerically(">", gen1))

			bumpSpec(func(s *releasesv1alpha1.PlatformSpec) {
				delete(s.Registry, "opmodel.dev/catalogs/k8s@v1")
			})
			gen3 := reconcileGeneration()
			Expect(gen3).To(BeNumerically(">", gen2))

			gens, err := r.Layout.Generations()
			Expect(err).NotTo(HaveOccurred())
			Expect(gens).To(Equal([]int64{gen3}), "only the current generation stays when no render leases an earlier one")
			for _, gen := range []int64{gen1, gen2} {
				_, statErr := os.Stat(r.Layout.Dir(gen))
				Expect(os.IsNotExist(statErr)).To(BeTrue(), "gen-%d should have been pruned", gen)
			}
		})

		It("keeps a leased superseded generation on disk and prunes it on the next reconcile once released", func() {
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

			var generations []int64
			reconcileCurrent := func() {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
				_, err := r.Reconcile(ctx, clusterRequest)
				Expect(err).NotTo(HaveOccurred())
				rec, ok := store.Generated()
				Expect(ok).To(BeTrue())
				Expect(rec.Generation).To(Equal(plat.Generation))
				generations = append(generations, plat.Generation)
			}
			bumpGeneration := func(typ string) {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
				plat.Spec.Type = typ
				Expect(k8sClient.Update(ctx, plat)).To(Succeed())
			}

			reconcileCurrent()
			// A render takes a lease on generation 1 (as the renderers do for
			// the duration of Kernel.Render) and is still reading its directory
			// when generation 2 lands.
			leased, release, ok := store.Lease()
			Expect(ok).To(BeTrue())
			Expect(leased.Generation).To(Equal(generations[0]))

			bumpGeneration("kubernetes-2")
			reconcileCurrent()
			Expect(generations).To(HaveLen(2))
			Expect(generations[1]).To(BeNumerically(">", generations[0]))
			onDisk, err := r.Layout.Generations()
			Expect(err).NotTo(HaveOccurred())
			Expect(onDisk).To(Equal(generations), "a leased superseded generation survives the swap")
			Expect(store.Leased()).To(Equal([]int64{generations[0]}))

			// The render finishes; the next reconcile prunes the released
			// generation along with the now-unleased generation 2.
			release()
			Expect(store.Leased()).To(BeEmpty())
			bumpGeneration("kubernetes-3")
			reconcileCurrent()
			Expect(generations).To(HaveLen(3))
			onDisk, err = r.Layout.Generations()
			Expect(err).NotTo(HaveOccurred())
			Expect(onDisk).To(Equal(generations[2:]), "once released, a superseded generation is pruned by the next reconcile")
		})

		It("surfaces a nonexistent pin as Ready=False/BuildFailed naming path and version, keeping the last good record", func() {
			k, reg := buildKernelOrSkip()

			store := platformstore.NewStore()
			lastGood := generatedMarker(1)
			store.SetGenerated(lastGood) // pre-existing good platform must survive a failure

			r := newPlatformReconciler(store, k, reg)

			const bogus = "testing.opmodel.dev/catalogs/does-not-exist@v9"
			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{bogus: {Version: "9.9.9"}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			res, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0), "a failed build rechecks on a bounded interval")

			fetched := &releasesv1alpha1.Platform{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
			ready := readyCondition(fetched)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.BuildFailedReason))
			Expect(ready.Message).To(ContainSubstring(bogus+".9.9"), "message should name the failing path and version")
			Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))

			// Last-good platform is preserved on failure (§8.4 freeze posture).
			held, ok := store.Generated()
			Expect(ok).To(BeTrue())
			Expect(held.Platform).To(BeIdenticalTo(lastGood.Platform))
			Expect(store.Generation()).To(Equal(int64(1)))

			gens, gerr := r.Layout.Generations()
			Expect(gerr).NotTo(HaveOccurred())
			Expect(gens).To(BeEmpty(), "a closure failure writes no module directory")
		})

		It("surfaces a generation defect as Ready=False/BuildFailed naming the registry entry", func() {
			k, reg := buildKernelOrSkip()
			catalogPath := testCatalogPath()
			const pinned = "4.0.0" // a published build older than the fixture's
			if catalogPath != "opmodel.dev/catalogs/opm@v4" || fixtures.CatalogVersion() == pinned {
				registrySkip("generation-defect spec needs opmodel.dev/catalogs/opm@v4 with a fixture build newer than " + pinned)
			}

			// Model the defect through the closure: the pinned catalog's module
			// file is made to require a newer build of the catalog itself, so the
			// derived cue.mod pins the newer build while the entry stamps the
			// CR's version. D13's tripwire turns that into a conflict naming the
			// entry before anything renders against it.
			real, err := newTestModFileSource(reg)
			Expect(err).NotTo(HaveOccurred())
			store := platformstore.NewStore()
			r := newPlatformReconciler(store, k, reg)
			r.ModFiles = requiringSource{
				ModFileSource: real,
				target:        module.MustNewVersion(catalogPath, "v"+pinned),
				extra:         module.MustNewVersion(catalogPath, "v"+fixtures.CatalogVersion()),
			}

			plat := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
				Spec: releasesv1alpha1.PlatformSpec{
					Type:     "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{catalogPath: {Version: pinned}},
				},
			}
			Expect(k8sClient.Create(ctx, plat)).To(Succeed())

			_, err = r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())

			fetched := &releasesv1alpha1.Platform{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), fetched)).To(Succeed())
			ready := readyCondition(fetched)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.BuildFailedReason))
			Expect(ready.Message).To(ContainSubstring(`#registry."`+catalogPath+`".version`), "message should name the registry entry")
			Expect(ready.Message).To(ContainSubstring(pinned))
			Expect(ready.Message).To(ContainSubstring(fixtures.CatalogVersion()))

			_, ok := store.Generated()
			Expect(ok).To(BeFalse(), "a module that fails to build is never recorded")
		})
	})

	Context("stored legacy singleton (versionless subscription)", func() {
		It("surfaces the missing version as BuildFailed naming the subscription path", func() {
			// A versionless subscription cannot be created through admission —
			// version is CRD-required — so it exists only as a stored object
			// predating the filter→version reshape, which validation
			// ratcheting keeps status-patchable. Seed a fake client with that
			// stored shape; the reconciler refuses it before any registry I/O,
			// so no registry is needed.
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
				Layout:        platformstore.Layout{Root: GinkgoT().TempDir()},
			}

			res, err := r.Reconcile(ctx, clusterRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0), "a stalled Platform rechecks on a bounded interval")

			fetched := &releasesv1alpha1.Platform{}
			Expect(c.Get(ctx, client.ObjectKey{Name: platformSingletonName}, fetched)).To(Succeed())
			ready := readyCondition(fetched)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.BuildFailedReason))
			Expect(ready.Message).To(ContainSubstring(legacyPath), "message should name the versionless subscription")
			Expect(ready.Message).To(ContainSubstring("version is required"))
			Expect(fetched.Status.ObservedGeneration).To(Equal(plat.Generation))

			_, held := store.Generated()
			Expect(held).To(BeFalse(), "nothing must be recorded for a versionless subscription")
		})
	})
})
