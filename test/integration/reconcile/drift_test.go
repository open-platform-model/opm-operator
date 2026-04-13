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

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/status"
)

var _ = Describe("Drift Detection", func() {
	Context("When drift is detected during reconcile", func() {
		It("should set Drifted=True condition on first apply", func() {
			createReadyOCIRepository("drift-detect-repo")
			createModuleRelease("drift-detect-mr", "drift-detect-repo")

			params := reconcileParams(&copyDirFetcher{
				sourceDir: renderTestdataDir("valid-module"),
			})

			nn := types.NamespacedName{Name: "drift-detect-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources.
			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify Ready=True and no drift.
			var mr releasesv1alpha1.ModuleRelease
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
				var latest releasesv1alpha1.ModuleRelease
				if err := k8sClient.Get(ctx, nn, &latest); err != nil {
					return err
				}
				latest.Status.LastAppliedSourceDigest = "sha256:changed"
				return k8sClient.Status().Update(ctx, &latest)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Second reconcile — detects drift, then applies (clearing it).
			result, err = opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-detect-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-detect-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("When drift is detected on no-op reconcile", func() {
		It("should set Drifted=True and preserve Ready=True", func() {
			createReadyOCIRepository("drift-noop-repo")
			createModuleRelease("drift-noop-mr", "drift-noop-repo")

			params := reconcileParams(&copyDirFetcher{
				sourceDir: renderTestdataDir("valid-module"),
			})

			nn := types.NamespacedName{Name: "drift-noop-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources.
			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify initial state is Ready=True.
			var mr releasesv1alpha1.ModuleRelease
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
			result, err = opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-noop-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-noop-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("When drift detection itself fails", func() {
		It("should increment failureCounters.drift and not set Drifted condition", func() {
			createReadyOCIRepository("drift-fail-repo")
			createModuleRelease("drift-fail-mr", "drift-fail-repo")

			fetcher := &copyDirFetcher{sourceDir: renderTestdataDir("valid-module")}
			params := reconcileParams(fetcher)

			nn := types.NamespacedName{Name: "drift-fail-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources normally.
			_, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).To(BeNil())

			By("constructing a ResourceManager with a client that fails on Patch (SSA dry-run)")
			realClient, err := client.NewWithWatch(cfg, client.Options{})
			Expect(err).NotTo(HaveOccurred())

			failingClient := interceptor.NewClient(realClient, interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("injected dry-run failure")
				},
			})

			failingParams := &opmreconcile.ModuleReleaseParams{
				Client:          k8sClient,
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(failingClient, "opm-controller"),
				ArtifactFetcher: fetcher,
			}

			// Second reconcile — drift detection fails, but reconcile continues as no-op.
			_, err = opmreconcile.ReconcileModuleRelease(ctx, failingParams, ctrl.Request{
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-fail-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-fail-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("When apply resolves drift", func() {
		It("should clear Drifted condition after successful apply", func() {
			createReadyOCIRepository("drift-clear-repo")
			createModuleRelease("drift-clear-mr", "drift-clear-repo")

			params := reconcileParams(&copyDirFetcher{
				sourceDir: renderTestdataDir("valid-module"),
			})

			nn := types.NamespacedName{Name: "drift-clear-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// First reconcile — applies resources.
			_, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
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
			_, err = opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			drifted := apimeta.FindStatusCondition(mr.Status.Conditions, status.DriftedCondition)
			Expect(drifted).NotTo(BeNil(), "drift should be detected")

			// Change the source digest to force a real apply.
			Eventually(func() error {
				var latest releasesv1alpha1.ModuleRelease
				if err := k8sClient.Get(ctx, nn, &latest); err != nil {
					return err
				}
				latest.Status.LastAppliedSourceDigest = "sha256:force-apply"
				return k8sClient.Status().Update(ctx, &latest)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Apply reconcile — should clear drift.
			_, err = opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-clear-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "drift-clear-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})
})
