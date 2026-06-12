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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/materialize"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// platformGatedStubRenderer mirrors KernelModuleRenderer's platform gate without
// needing an OCI registry: it returns render.ErrPlatformNotReady while the store
// holds no materialized platform, and a stub ConfigMap result once it does. This
// lets a manager-driven test prove the Platform watch re-enqueues a release
// blocked on PlatformNotReady the moment a Platform appears.
type platformGatedStubRenderer struct {
	store *platformstore.Store
}

func (r *platformGatedStubRenderer) RenderModule(
	_ context.Context,
	_, ns, _, _ string,
	values *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	if _, ok := r.store.Get(); !ok {
		return nil, render.ErrPlatformNotReady
	}
	return stubRenderResult(ns, values), nil
}

// This is the end-to-end counterpart to the unit-level mapPlatformToModuleReleases
// test: it runs a real manager so the Platform watch (and the generation
// predicate that now lives on For(), not as a global event filter) actually
// fire, proving a release blocked on PlatformNotReady recovers on a Platform
// change rather than only on the stalled-recheck backoff.
//
// Isolation: the release lives in a dedicated namespace so its rendered
// "test-module" ConfigMap (the stub render result, which carries a fixed release
// UUID shared by every stub) cannot be pruned by another spec's deletion path.
var _ = Describe("Platform-gated re-enqueue (manager-driven)", func() {
	const (
		gateNamespace = "platform-gate-ns"
		mrName        = "platform-gate-mr"
		// The Platform is a cluster singleton; "cluster" is the only name the
		// CRD validation permits. This spec is the only one in the suite that
		// creates a Platform, so there is no contention.
		platformName = "cluster"
	)

	It("unblocks a PlatformNotReady release when a Platform appears", func() {
		mgrCtx, cancelMgr := context.WithCancel(ctx)
		defer cancelMgr()

		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: gateNamespace},
		}))).To(Succeed())

		// A sibling spec also runs a manager wiring a controller named
		// "modulerelease"; controller-runtime enforces process-global name
		// uniqueness, so skip that check for this test-only manager.
		skipNameValidation := true
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			LeaderElection:         false,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Controller:             config.Controller{SkipNameValidation: &skipNameValidation},
		})
		Expect(err).NotTo(HaveOccurred())

		// Empty store → renderer gates with ErrPlatformNotReady until we Set it.
		store := platformstore.NewStore()

		reconciler := &opmcontroller.ModuleReleaseReconciler{
			Client:          mgr.GetClient(),
			APIReader:       mgr.GetAPIReader(),
			Scheme:          mgr.GetScheme(),
			RestConfig:      cfg,
			ResourceManager: apply.NewResourceManager(mgr.GetClient(), "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Renderer:        &platformGatedStubRenderer{store: store},
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			_ = mgr.Start(mgrCtx)
		}()

		nn := types.NamespacedName{Name: mrName, Namespace: gateNamespace}
		cmName := types.NamespacedName{Name: "test-module", Namespace: gateNamespace}
		mr := &releasesv1alpha1.ModuleRelease{
			ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: gateNamespace},
			Spec: releasesv1alpha1.ModuleReleaseSpec{
				Module: releasesv1alpha1.ModuleReference{
					Path:    "opmodel.dev/test/module",
					Version: "v0.1.0",
				},
				Values: &releasesv1alpha1.RawValues{},
			},
		}
		mr.Spec.Values.Raw = []byte(`{"message":"gated"}`)
		Expect(k8sClient.Create(ctx, mr)).To(Succeed())

		// Phase 1: with no platform, the release blocks on PlatformNotReady and
		// applies nothing. The reconcile requeues only on the 30-minute
		// stalled-recheck, so anything that unblocks it within the test window
		// must come from the Platform watch.
		Eventually(func(g Gomega) {
			var current releasesv1alpha1.ModuleRelease
			g.Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			ready := meta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Reason).To(Equal(status.PlatformNotReadyReason))
		}).WithTimeout(10 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())

		// Nothing applied while blocked.
		Expect(k8sClient.Get(ctx, cmName, &corev1.ConfigMap{})).
			To(MatchError(ContainSubstring("not found")))

		// Phase 2: materialize the platform in the store, then apply the Platform
		// CR. The CR change triggers the watch → mapPlatformToModuleReleases
		// re-enqueues the blocked release; the store is now populated, so the
		// renderer succeeds and the resources are applied. The watch enqueues
		// every release on any Platform change, independent of the Platform's
		// contents.
		store.Set(1, &materialize.MaterializedPlatform{})
		platform := &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: platformName},
			Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
		}
		Expect(k8sClient.Create(ctx, platform)).To(Succeed())

		Eventually(func(g Gomega) {
			var current releasesv1alpha1.ModuleRelease
			g.Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			ready := meta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue),
				"release must reconcile to Ready after the Platform watch re-enqueues it")
		}).WithTimeout(10 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())

		// The rendered ConfigMap is now applied.
		Eventually(func() error {
			return k8sClient.Get(ctx, cmName, &corev1.ConfigMap{})
		}).WithTimeout(10 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())

		// Cleanup.
		Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
			ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: gateNamespace},
		})).To(Succeed())
		Eventually(func() bool {
			var current releasesv1alpha1.ModuleRelease
			return k8sClient.Get(ctx, nn, &current) != nil
		}).WithTimeout(10 * time.Second).WithPolling(250 * time.Millisecond).Should(BeTrue())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: gateNamespace},
		}))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: platformName},
		}))).To(Succeed())
	})
})
