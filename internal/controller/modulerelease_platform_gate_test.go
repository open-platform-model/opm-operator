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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-platform-model/library/opm/kernel"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

var _ = Describe("ModuleRelease platform-gated rendering", func() {
	const namespace = "default"

	newModuleRelease := func(ctx context.Context, name string) types.NamespacedName {
		mr := &releasesv1alpha1.ModuleRelease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: releasesv1alpha1.ModuleReleaseSpec{
				Module: releasesv1alpha1.ModuleReference{
					Path:    "opmodel.dev/test/module",
					Version: "v0.1.0",
				},
				Values: &releasesv1alpha1.RawValues{},
			},
		}
		mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
		Expect(k8sClient.Create(ctx, mr)).To(Succeed())
		return types.NamespacedName{Name: name, Namespace: namespace}
	}

	Context("when a platform is materialized (5.1)", func() {
		It("renders the module and applies the resulting resources", func() {
			ctx := context.Background()

			// The render+apply success path runs through the ModuleRenderer seam
			// that the cut-over swaps to KernelModuleRenderer. Exercising the
			// kernel's acquire→synthesize→compile internals requires a live OCI
			// registry and a materialized platform, so those are covered by the
			// renderer's own tests; here we assert the reconciler applies and
			// records status when rendering succeeds (the platform-present case).
			nn := newModuleRelease(ctx, "gate-apply-mr")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// First reconcile adds the finalizer; second runs the full pipeline.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(mr.Status.Inventory).NotTo(BeNil())
			Expect(mr.Status.Inventory.Count).To(Equal(int64(1)))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &mr)).To(Succeed())
		})
	})

	Context("when no platform is materialized (5.2)", func() {
		It("blocks with PlatformNotReady, applying and pruning nothing", func() {
			ctx := context.Background()

			// Use the real KernelModuleRenderer with an empty store. Store.Get()
			// reports no platform, so RenderModule returns ErrPlatformNotReady
			// before any registry I/O — exercising the actual sentinel through
			// the gate, no OCI registry required.
			nn := newModuleRelease(ctx, "gate-block-mr")

			recorder := events.NewFakeRecorder(10)
			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   recorder,
				Renderer: &render.KernelModuleRenderer{
					Kernel:      kernel.New(),
					Store:       platformstore.NewStore(), // empty: no materialized platform
					RuntimeName: "opm-controller",
				},
			}

			// First reconcile adds the finalizer; second hits the gate.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "blocked-on-platform returns nil error with requeue")
			Expect(result.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())

			// Ready=False with reason PlatformNotReady — distinct from a render
			// failure (RenderFailed/ResolutionFailed) and not a terminal stall.
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.PlatformNotReadyReason))
			Expect(ready.Reason).NotTo(Equal(status.RenderFailedReason))
			Expect(ready.Reason).NotTo(Equal(status.ResolutionFailedReason))

			// Blocked, not stalled.
			stalled := apimeta.FindStatusCondition(mr.Status.Conditions, status.StalledCondition)
			Expect(stalled).To(BeNil())

			// Nothing applied: no inventory recorded and no ConfigMap created.
			Expect(mr.Status.Inventory).To(BeNil())
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &corev1.ConfigMap{})
			Expect(err).To(HaveOccurred())

			// A warning event was emitted for the blocked state.
			var event string
			Eventually(recorder.Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring(status.PlatformNotReadyReason))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &mr)).To(Succeed())
		})
	})

	Context("when the platform changes (5.3)", func() {
		It("re-enqueues every ModuleRelease in the cluster", func() {
			ctx := context.Background()

			nn1 := newModuleRelease(ctx, "gate-enqueue-mr-1")
			nn2 := newModuleRelease(ctx, "gate-enqueue-mr-2")

			reconciler := &ModuleReleaseReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
			}

			platform := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			reqs := reconciler.mapPlatformToModuleReleases(ctx, platform)

			Expect(reqs).To(ContainElement(reconcile.Request{NamespacedName: nn1}))
			Expect(reqs).To(ContainElement(reconcile.Request{NamespacedName: nn2}))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: nn1.Name, Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: nn2.Name, Namespace: namespace},
			})).To(Succeed())
		})
	})
})
