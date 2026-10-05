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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

var _ = Describe("Drift Detection", func() {
	Context("When drift is detected during reconcile", func() {
		It("should set Drifted=True condition on first apply", func() {
			createModuleInstance("drift-detect-mr")

			params := reconcileParams()

			nn := types.NamespacedName{Name: "drift-detect-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources.
			result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify Ready=True and no drift.
			var mr releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			drifted := apimeta.FindStatusCondition(mr.Status.Conditions, status.DriftedCondition)
			Expect(drifted).To(BeNil(), "no drift on fresh apply")

			// Modify the ConfigMap on the cluster to create drift.
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, cm)).To(Succeed())
			cm.Data["message"] = "drifted-by-hand"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			// Change the source digest to force a non-no-op reconcile.
			Eventually(func() error {
				var latest releasesv1alpha1.ModuleInstance
				if err := k8sClient.Get(ctx, nn, &latest); err != nil {
					return err
				}
				latest.Status.LastAppliedSourceDigest = "sha256:changed"
				return k8sClient.Status().Update(ctx, &latest)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Second reconcile — detects drift, then applies (clearing it).
			result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			// After apply, drift should be cleared.
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			drifted = apimeta.FindStatusCondition(mr.Status.Conditions, status.DriftedCondition)
			Expect(drifted).To(BeNil(), "drift cleared after successful apply")

			ready = apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-detect-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("When drift is detected on no-op reconcile", func() {
		It("should set Drifted=True and preserve Ready=True", func() {
			createModuleInstance("drift-noop-mr")

			params := reconcileParams()

			nn := types.NamespacedName{Name: "drift-noop-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources.
			result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify initial state is Ready=True.
			var mr releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// Modify the ConfigMap on the cluster to create drift.
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, cm)).To(Succeed())
			cm.Data["message"] = "drifted-on-noop"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			// Second reconcile — digests unchanged, so this is a no-op.
			// But drift detection should still run.
			result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())

			// Drifted=True should be set.
			drifted := apimeta.FindStatusCondition(mr.Status.Conditions, status.DriftedCondition)
			Expect(drifted).NotTo(BeNil())
			Expect(drifted.Status).To(Equal(metav1.ConditionTrue))
			Expect(drifted.Reason).To(Equal(status.DriftDetectedReason))

			// Ready=True should be preserved (drift is informational).
			ready = apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-noop-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("When drift detection itself fails", func() {
		It("should increment failureCounters.drift and not set Drifted condition", func() {
			createModuleInstance("drift-fail-mr")

			params := reconcileParams()

			nn := types.NamespacedName{Name: "drift-fail-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources normally.
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			var mr releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Drift).To(Equal(int64(0)))

			By("constructing a ResourceManager with a client that fails on Patch (SSA dry-run)")
			realClient, err := client.NewWithWatch(cfg, client.Options{})
			Expect(err).NotTo(HaveOccurred())

			failingClient := interceptor.NewClient(realClient, interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("injected dry-run failure")
				},
			})

			failingParams := &opmreconcile.ModuleInstanceParams{
				Client:          k8sClient,
				ResourceManager: apply.NewResourceManager(failingClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// Second reconcile — drift detection fails, but reconcile continues as no-op.
			_, err = opmreconcile.ReconcileModuleInstance(ctx, failingParams, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred(), "drift failure is non-blocking")

			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())

			// failureCounters.drift should be incremented.
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Drift).To(Equal(int64(1)))

			// Drifted condition should NOT be set (unknown state).
			drifted := apimeta.FindStatusCondition(mr.Status.Conditions, status.DriftedCondition)
			Expect(drifted).To(BeNil(), "Drifted condition should not be set on failure")

			// Ready=True should be preserved.
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-fail-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("When apply resolves drift", func() {
		It("should clear Drifted condition after successful apply", func() {
			createModuleInstance("drift-clear-mr")

			params := reconcileParams()

			nn := types.NamespacedName{Name: "drift-clear-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources.
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			// Modify the ConfigMap to create drift.
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, cm)).To(Succeed())
			cm.Data["message"] = "drifted"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			// No-op reconcile — detects drift, sets condition.
			_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			var mr releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			drifted := apimeta.FindStatusCondition(mr.Status.Conditions, status.DriftedCondition)
			Expect(drifted).NotTo(BeNil(), "drift should be detected")

			// Change the source digest to force a real apply.
			Eventually(func() error {
				var latest releasesv1alpha1.ModuleInstance
				if err := k8sClient.Get(ctx, nn, &latest); err != nil {
					return err
				}
				latest.Status.LastAppliedSourceDigest = "sha256:force-apply"
				return k8sClient.Status().Update(ctx, &latest)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Apply reconcile — should clear drift.
			_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			drifted = apimeta.FindStatusCondition(mr.Status.Conditions, status.DriftedCondition)
			Expect(drifted).To(BeNil(), "drift should be cleared after apply")

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-clear-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})
})

// Drift detection under the render skip (render-input-key): a reconcile that
// skips its render sends no dry-run and leaves Drifted as it was; once the
// last confirming render is older than the drift render interval, the next
// reconcile renders and finds the drift.
var _ = Describe("Drift Detection with the render skip", func() {
	It("finds no drift while the render is skipped, and finds it once the interval has passed", func() {
		const name = "drift-skip-mr"
		plat := &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
		}
		Expect(k8sClient.Create(ctx, plat)).To(Succeed())
		DeferCleanup(func(ctx context.Context) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}))).To(Succeed())
		})
		Eventually(func(g Gomega) {
			var current releasesv1alpha1.Platform
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, &current)).To(Succeed())
			current.Status.PackageIdentity = "gen-1"
			g.Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		createModuleInstance(name)
		nn := types.NamespacedName{Name: name, Namespace: namespace}
		DeferCleanup(func(ctx context.Context) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			}))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			}))).To(Succeed())
		})

		// The render reports the platform the Platform CR names, so the
		// recorded key matches the pre-render key on the next reconcile.
		values := &releasesv1alpha1.RawValues{}
		values.Raw = []byte(`{"message": "hello"}`)
		result := stubRenderResult(namespace, values)
		result.PlatformIdentity = "gen-1"
		result.SkewPolicy = releasesv1alpha1.SkewPolicyWarn

		// Count every patch the resource manager sends: drift detection is a
		// server-side apply dry-run, a patch.
		realClient, err := client.NewWithWatch(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		var patches int
		counting := interceptor.NewClient(realClient, interceptor.Funcs{
			Patch: func(
				ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption,
			) error {
				patches++
				return c.Patch(ctx, obj, p, opts...)
			},
		})
		params := reconcileParams()
		params.Renderer = &stubRenderer{result: result}
		params.ResourceManager = apply.NewResourceManager(counting, "opm-controller")
		params.OperatorVersion = "v1.0.0-test"
		params.LibraryVersion = "v1.0.0-test.library"
		params.DriftRenderInterval = 30 * time.Minute

		ensureFinalizer(params, nn)
		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.LastAppliedInputs).NotTo(BeNil(), "the apply records the key")

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, cm)).To(Succeed())
		cm.Data["message"] = "drifted-under-skip"
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())

		By("reconciling within the interval: the render is skipped")
		patches = 0
		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(patches).To(BeZero(), "a skipped reconcile sends no dry-run")
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)).To(BeNil(),
			"Drifted is left as it was")

		By("reconciling after the interval: the render runs and finds the drift")
		Eventually(func(g Gomega) {
			var current releasesv1alpha1.ModuleInstance
			g.Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Status.LastAppliedInputs.RenderedAt = metav1.NewTime(time.Now().Add(-31 * time.Minute))
			g.Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(patches).To(BeNumerically(">", 0), "the render sent its dry-run")
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		drifted := apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
		Expect(drifted).NotTo(BeNil())
		Expect(drifted.Status).To(Equal(metav1.ConditionTrue))
		Expect(drifted.Reason).To(Equal(status.DriftDetectedReason))
	})
})
