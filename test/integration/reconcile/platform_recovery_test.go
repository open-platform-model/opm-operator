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
	"io/fs"
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

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/platformmodule"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// recoveryPlatformName is the cluster singleton — the only name the CRD permits.
const recoveryPlatformName = "cluster"

// liveBuildKernelOrSkip builds a Kernel from CUE_REGISTRY and skips unless the
// core the generated platform module pins resolves from it, i.e. the registry
// is reachable. The recovery spec needs a subscription that actually pulls
// from the registry (so a dead endpoint fails and a live one succeeds), which
// is why it also requires a catalog fixture: OPM_TEST_CATALOG_PATH when set
// (seeded CI), the first-party abstraction catalog otherwise.
func liveBuildKernelOrSkip() (*kernel.Kernel, string, string) {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		registrySkip("CUE_REGISTRY not set — platform recovery spec needs a reachable registry with opmodel.dev/core@v2")
	}
	catalogPath := os.Getenv("OPM_TEST_CATALOG_PATH")
	if catalogPath == "" {
		catalogPath = "opmodel.dev/catalogs/opm@v4"
	}
	src, err := platformmodule.NewRegistry(reg)
	Expect(err).NotTo(HaveOccurred())
	entries := []platformmodule.Entry{{Path: catalogPath, Version: testCatalogVersion(), Enable: true}}
	if _, err := platformmodule.Closure(ctx, src, platformmodule.Roots(entries)); err != nil {
		registrySkip("core and catalog not resolvable from CUE_REGISTRY: " + err.Error())
	}
	return kernel.New(kernel.WithRegistry(reg)), reg, catalogPath
}

// This is the registry-backed counterpart to the unit-level failure specs in
// internal/controller: it proves the "Recovery without a spec change" scenario
// end-to-end. The same Platform CR (same generation) reconciles to
// BuildFailed against an unreachable registry, then — with no edit to the CR
// — reconciles to Ready once the registry condition clears. The reconcile
// loop self-heals on its own requeue; nothing re-triggers the generation
// predicate.
//
// The "registry condition clears" is modelled by swapping the reconciler's
// registry seams (Kernel, Registry mapping and module-file source) from ones
// pointed at a dead endpoint to ones pointed at the working registry.
// WithRegistry is construction-only, so a fresh Kernel is the reconciler's only
// window onto a recovered registry; the Platform CR and its generation stay
// untouched throughout, which is the property the scenario asserts.
var _ = Describe("Platform build recovery (registry-backed)", func() {
	It("recovers a BuildFailed platform without a spec change once the registry clears", func() {
		// Gate on a reachable registry + resolvable catalog first, so the spec
		// skips cleanly when no registry is configured (same posture as the
		// other registry-backed specs in this suite).
		liveKernel, liveRegistry, catalogPath := liveBuildKernelOrSkip()

		// Defensive: drop any cluster Platform a sibling spec may have left.
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
		}))).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: recoveryPlatformName}, &releasesv1alpha1.Platform{})
			return err != nil && client.IgnoreNotFound(err) == nil
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(BeTrue())

		plat := &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
			Spec: releasesv1alpha1.PlatformSpec{
				Type:     "kubernetes",
				Registry: map[string]releasesv1alpha1.Subscription{catalogPath: {Version: testCatalogVersion()}},
			},
		}
		Expect(k8sClient.Create(ctx, plat)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
		generation := plat.Generation
		Expect(generation).NotTo(BeZero())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
			}))).To(Succeed())
		})

		store := platformstore.NewStore()
		// A registry mapping pointed at a closed port: module-file and catalog
		// resolution get a connection failure, modelling an unreachable registry.
		const deadRegistry = "opmodel.dev=localhost:1+insecure,testing.opmodel.dev=localhost:1+insecure"
		r := &opmcontroller.PlatformReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: events.NewFakeRecorder(10),
			Kernel:        kernel.New(kernel.WithRegistry(deadRegistry)),
			Store:         store,
			Registry:      deadRegistry,
			Layout:        platformmodule.Layout{Root: filepath.Join(GinkgoT().TempDir(), "platform")},
		}
		req := ctrl.Request{NamespacedName: client.ObjectKey{Name: recoveryPlatformName}}

		// Phase 1: registry unreachable → BuildFailed, requeued, store empty.
		//
		// A dead endpoint alone does not model an unreachable registry: the
		// closure derivation and the build pull the exact named builds, and the
		// CUE module cache satisfies those pulls without touching the endpoint
		// (the gating closure above has just warmed it). Point CUE_CACHE_DIR at
		// an empty directory for this phase only, so the dead endpoint is
		// actually consulted; the environment is restored before the recovery
		// phase, whose live pull must see the same process environment the
		// other specs use.
		origCache, hadCache := os.LookupEnv("CUE_CACHE_DIR")
		restoreCache := func() {
			if hadCache {
				Expect(os.Setenv("CUE_CACHE_DIR", origCache)).To(Succeed())
			} else {
				Expect(os.Unsetenv("CUE_CACHE_DIR")).To(Succeed())
			}
		}
		DeferCleanup(restoreCache)
		// Not GinkgoT().TempDir(): CUE marks extracted cache files read-only,
		// which breaks Ginkgo's automatic removal. Restore write permission
		// before removing, best-effort.
		emptyCache, err := os.MkdirTemp("", "opm-dead-registry-cache-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = filepath.WalkDir(emptyCache, func(p string, _ fs.DirEntry, walkErr error) error {
				if walkErr == nil {
					_ = os.Chmod(p, 0o755)
				}
				return nil
			})
			_ = os.RemoveAll(emptyCache)
		})
		Expect(os.Setenv("CUE_CACHE_DIR", emptyCache)).To(Succeed())

		res, err := r.Reconcile(ctx, req)
		restoreCache()
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0),
			"a failed build must requeue rather than stall indefinitely")

		failed := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), failed)).To(Succeed())
		ready := apimeta.FindStatusCondition(failed.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.BuildFailedReason))
		Expect(failed.Status.ObservedGeneration).To(Equal(generation),
			"observedGeneration must be recorded on the failure path")
		_, held := store.Generated()
		Expect(held).To(BeFalse(), "nothing should be recorded while the build is failing")

		// Phase 2: the registry condition clears — no edit to the Platform CR.
		// Swap in the working registry seams and reconcile the same object
		// again. The module-file source is dropped so it is rebuilt from the
		// live mapping on the next reconcile.
		r.Kernel = liveKernel
		r.Registry = liveRegistry
		r.ModFiles = nil

		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeZero(), "a successful build does not requeue")

		recovered := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), recovered)).To(Succeed())
		Expect(recovered.Generation).To(Equal(generation),
			"recovery must happen without a spec change (generation unchanged)")
		ready = apimeta.FindStatusCondition(recovered.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal(status.GeneratedReason))
		Expect(recovered.Status.ObservedGeneration).To(Equal(generation))

		got, held := store.Generated()
		Expect(held).To(BeTrue(), "the recovered platform must be recorded in the store")
		Expect(got.Platform).NotTo(BeNil())
		Expect(got.Generation).To(Equal(generation))
		Expect(got.Dir).To(Equal(r.Layout.Dir(generation)))
		Expect(store.Generation()).To(Equal(generation))
	})
})
