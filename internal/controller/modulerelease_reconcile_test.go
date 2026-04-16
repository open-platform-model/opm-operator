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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/status"
)

var _ = Describe("ModuleRelease Reconcile Loop", func() {
	const namespace = "default"

	createModuleRelease := func(ctx context.Context, name string) *releasesv1alpha1.ModuleRelease {
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
		return mr
	}

	Context("Full reconcile pipeline", func() {
		It("should apply resources and populate status on first reconcile", func() {
			ctx := context.Background()

			createModuleRelease(ctx, "full-reconcile-mr")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "full-reconcile-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Second reconcile runs the full pipeline.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify the ConfigMap was created by SSA.
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-module",
				Namespace: namespace,
			}, &cm)).To(Succeed())
			Expect(cm.Data["message"]).To(Equal("hello"))

			// Verify status was populated.
			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "full-reconcile-mr",
				Namespace: namespace,
			}, &mr)).To(Succeed())

			// Finalizer preserved after normal reconcile.
			Expect(controllerutil.ContainsFinalizer(&mr, opmreconcile.FinalizerName)).To(BeTrue())

			// Ready=True
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// ModuleResolved=True
			moduleResolved := apimeta.FindStatusCondition(mr.Status.Conditions, status.ModuleResolvedCondition)
			Expect(moduleResolved).NotTo(BeNil())
			Expect(moduleResolved.Status).To(Equal(metav1.ConditionTrue))

			// Digests populated
			Expect(mr.Status.LastAppliedSourceDigest).NotTo(BeEmpty())
			Expect(mr.Status.LastAppliedConfigDigest).NotTo(BeEmpty())
			Expect(mr.Status.LastAppliedRenderDigest).NotTo(BeEmpty())
			Expect(mr.Status.LastAttemptedSourceDigest).NotTo(BeEmpty())

			// Inventory populated
			Expect(mr.Status.Inventory).NotTo(BeNil())
			Expect(mr.Status.Inventory.Count).To(Equal(int64(1)))
			Expect(mr.Status.Inventory.Entries).To(HaveLen(1))
			Expect(mr.Status.Inventory.Entries[0].Kind).To(Equal("ConfigMap"))
			Expect(mr.Status.Inventory.Digest).NotTo(BeEmpty())

			// History populated
			Expect(mr.Status.History).NotTo(BeEmpty())
			Expect(mr.Status.History[0].Action).To(Equal("reconcile"))
			Expect(mr.Status.History[0].Phase).To(Equal("complete"))

			// ObservedGeneration set
			Expect(mr.Status.ObservedGeneration).To(Equal(mr.Generation))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "full-reconcile-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Suspend check", func() {
		It("should skip reconciliation when suspend is true and set correct conditions", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspended-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: record.NewFakeRecorder(10),
				Renderer:      &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "suspended-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Second reconcile hits suspend.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify conditions: Ready=False/Suspended, Reconciling removed, Stalled removed.
			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.SuspendedReason))
			Expect(ready.Message).To(Equal("Reconciliation is suspended"))

			reconciling := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReconcilingCondition)
			Expect(reconciling).To(BeNil())

			stalled := apimeta.FindStatusCondition(updated.Status.Conditions, status.StalledCondition)
			Expect(stalled).To(BeNil())

			// Cleanup
			Expect(k8sClient.Delete(ctx, mr)).To(Succeed())
		})

		It("should preserve existing status when suspend is true", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspend-preserve-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "suspend-preserve-mr", Namespace: namespace}

			// Finalizer reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Full reconcile — applies resources and populates status.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Capture status after successful reconcile.
			var beforeSuspend releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &beforeSuspend)).To(Succeed())
			Expect(beforeSuspend.Status.Inventory).NotTo(BeNil())
			Expect(beforeSuspend.Status.LastAppliedSourceDigest).NotTo(BeEmpty())
			Expect(beforeSuspend.Status.History).NotTo(BeEmpty())

			savedInventory := beforeSuspend.Status.Inventory.DeepCopy()
			savedAppliedSourceDigest := beforeSuspend.Status.LastAppliedSourceDigest
			savedAppliedConfigDigest := beforeSuspend.Status.LastAppliedConfigDigest
			savedAppliedRenderDigest := beforeSuspend.Status.LastAppliedRenderDigest
			savedAttemptedSourceDigest := beforeSuspend.Status.LastAttemptedSourceDigest
			savedAttemptedConfigDigest := beforeSuspend.Status.LastAttemptedConfigDigest
			savedAttemptedRenderDigest := beforeSuspend.Status.LastAttemptedRenderDigest
			savedHistoryLen := len(beforeSuspend.Status.History)

			// Set suspend=true.
			var current releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = true
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Reconcile while suspended.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify status is preserved.
			var afterSuspend releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &afterSuspend)).To(Succeed())

			Expect(afterSuspend.Status.Inventory).NotTo(BeNil())
			Expect(afterSuspend.Status.Inventory.Revision).To(Equal(savedInventory.Revision))
			Expect(afterSuspend.Status.Inventory.Digest).To(Equal(savedInventory.Digest))
			Expect(afterSuspend.Status.Inventory.Count).To(Equal(savedInventory.Count))
			Expect(afterSuspend.Status.LastAppliedSourceDigest).To(Equal(savedAppliedSourceDigest))
			Expect(afterSuspend.Status.LastAppliedConfigDigest).To(Equal(savedAppliedConfigDigest))
			Expect(afterSuspend.Status.LastAppliedRenderDigest).To(Equal(savedAppliedRenderDigest))
			Expect(afterSuspend.Status.LastAttemptedSourceDigest).To(Equal(savedAttemptedSourceDigest))
			Expect(afterSuspend.Status.LastAttemptedConfigDigest).To(Equal(savedAttemptedConfigDigest))
			Expect(afterSuspend.Status.LastAttemptedRenderDigest).To(Equal(savedAttemptedRenderDigest))
			Expect(afterSuspend.Status.History).To(HaveLen(savedHistoryLen))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "suspend-preserve-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should perform full reconcile when unsuspended", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "resume-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values:  &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "resume-mr", Namespace: namespace}

			// Finalizer reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile hits suspend — no source resolution, no apply.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify suspended state.
			var suspended releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &suspended)).To(Succeed())
			ready := apimeta.FindStatusCondition(suspended.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(status.SuspendedReason))

			// Unsuspend.
			var current releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = false
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Reconcile after unsuspend — should perform full reconcile.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify full reconcile happened: Ready=True, resources applied.
			var resumed releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &resumed)).To(Succeed())

			readyAfter := apimeta.FindStatusCondition(resumed.Status.Conditions, status.ReadyCondition)
			Expect(readyAfter).NotTo(BeNil())
			Expect(readyAfter.Status).To(Equal(metav1.ConditionTrue))

			// Inventory populated from the full reconcile.
			Expect(resumed.Status.Inventory).NotTo(BeNil())
			Expect(resumed.Status.Inventory.Count).To(Equal(int64(1)))

			// ConfigMap was applied.
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)).To(Succeed())
			Expect(cm.Data["message"]).To(Equal("hello"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "resume-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("No-op detection", func() {
		It("should skip apply on second reconcile when digests match", func() {
			ctx := context.Background()

			createModuleRelease(ctx, "noop-mr")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "noop-mr", Namespace: namespace}

			// Finalizer reconcile.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// First full reconcile — applies resources.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify first reconcile applied.
			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.LastAppliedSourceDigest).NotTo(BeEmpty())
			firstHistory := len(mr.Status.History)
			Expect(firstHistory).To(BeNumerically(">=", 1))

			// Second reconcile — should detect no-op.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify Ready=True and no new history entry (no-op doesn't record).
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// History count should remain the same (no-op skips recording).
			Expect(mr.Status.History).To(HaveLen(firstHistory))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "noop-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Finalizer registration", func() {
		It("should add finalizer on first reconcile", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-add-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: record.NewFakeRecorder(10),
				Renderer:      &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "finalizer-add-mr", Namespace: namespace}
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify finalizer was added.
			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&updated, opmreconcile.FinalizerName)).To(BeTrue())

			// Cleanup
			controllerutil.RemoveFinalizer(&updated, opmreconcile.FinalizerName)
			Expect(k8sClient.Update(ctx, &updated)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &updated)).To(Succeed())
		})
	})

	Context("Deletion with prune enabled", func() {
		It("should delete inventory resources and remove finalizer", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-prune-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "delete-prune-mr", Namespace: namespace}

			// Finalizer reconcile.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Full reconcile — applies the ConfigMap.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap exists.
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)).To(Succeed())

			// Delete the ModuleRelease (sets DeletionTimestamp, blocked by finalizer).
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-prune-mr", Namespace: namespace},
			})).To(Succeed())

			// Reconcile should run deletion cleanup: prune ConfigMap + remove finalizer.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify ConfigMap was deleted.
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			// Verify ModuleRelease is gone (finalizer removed, deletion completed).
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleRelease
				err := k8sClient.Get(ctx, nn, &deleted)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		})
	})

	Context("Deletion with prune disabled", func() {
		It("should remove finalizer without deleting resources", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-orphan-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune:  false,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "delete-orphan-mr", Namespace: namespace}

			// Finalizer + full reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap exists.
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)).To(Succeed())

			// Delete the ModuleRelease.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-orphan-mr", Namespace: namespace},
			})).To(Succeed())

			// Reconcile should remove finalizer without pruning.
			result, err2 := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err2).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify ConfigMap still exists (orphaned).
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)).To(Succeed())

			// Verify ModuleRelease is gone.
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleRelease
				err := k8sClient.Get(ctx, nn, &deleted)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
		})
	})

	Context("Deletion safety exclusions", func() {
		It("should skip Namespace and CRD during deletion cleanup", func() {
			ctx := context.Background()

			// Create a ModuleRelease with finalizer and fake inventory containing
			// a ConfigMap, a Namespace, and a CRD.
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "delete-safety-mr",
					Namespace:  namespace,
					Finalizers: []string{opmreconcile.FinalizerName},
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			// Create a ConfigMap that's in the inventory.
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "safety-test-cm",
					Namespace: namespace,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			// Patch status with inventory that includes ConfigMap, Namespace, and CRD.
			var latest releasesv1alpha1.ModuleRelease
			nn := types.NamespacedName{Name: "delete-safety-mr", Namespace: namespace}
			Expect(k8sClient.Get(ctx, nn, &latest)).To(Succeed())
			latest.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: 1,
				Count:    3,
				Entries: []releasesv1alpha1.InventoryEntry{
					{Group: "", Version: "v1", Kind: "ConfigMap", Namespace: namespace, Name: "safety-test-cm"},
					{Group: "", Version: "v1", Kind: "Namespace", Name: "safety-test-ns"},
					{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition", Name: "foos.example.com"},
				},
			}
			Expect(k8sClient.Status().Update(ctx, &latest)).To(Succeed())

			// Delete the ModuleRelease.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-safety-mr", Namespace: namespace},
			})).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: record.NewFakeRecorder(10),
				Renderer:      &stubRenderer{},
			}

			// Reconcile deletion.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// ConfigMap should be deleted.
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "safety-test-cm", Namespace: namespace,
			}, &corev1.ConfigMap{})
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			// ModuleRelease should be gone (finalizer removed).
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleRelease
				err := k8sClient.Get(ctx, nn, &deleted)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		})
	})

	Context("Deletion with suspend enabled", func() {
		It("should perform cleanup even when suspend is true", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-suspend-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "delete-suspend-mr", Namespace: namespace}

			// Finalizer + full reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap exists.
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)).To(Succeed())

			// Set suspend=true.
			var current releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = true
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Delete the ModuleRelease.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-suspend-mr", Namespace: namespace},
			})).To(Succeed())

			// Reconcile should still perform deletion cleanup despite suspend.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify ConfigMap was deleted.
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			// Verify ModuleRelease is gone.
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleRelease
				err := k8sClient.Get(ctx, nn, &deleted)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		})
	})

	Context("Deletion partial failure", func() {
		It("should retain finalizer when prune fails on some resources", func() {
			ctx := context.Background()

			// Create a ModuleRelease with finalizer and inventory containing
			// a resource with a non-existent GVK that will fail to delete.
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "delete-partial-fail-mr",
					Namespace:  namespace,
					Finalizers: []string{opmreconcile.FinalizerName},
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			// Patch status with inventory containing a resource that cannot be deleted
			// (non-existent GVK triggers a "no matches" error from the API server).
			nn := types.NamespacedName{Name: "delete-partial-fail-mr", Namespace: namespace}
			var latest releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &latest)).To(Succeed())
			latest.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: 1,
				Count:    1,
				Entries: []releasesv1alpha1.InventoryEntry{
					{
						Group:     "nonexistent.example.com",
						Version:   "v1",
						Kind:      "FakeResource",
						Namespace: namespace,
						Name:      "should-fail-delete",
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, &latest)).To(Succeed())

			// Delete the ModuleRelease.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-partial-fail-mr", Namespace: namespace},
			})).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: record.NewFakeRecorder(10),
				Renderer:      &stubRenderer{},
			}

			// Reconcile should fail — prune cannot delete the non-existent GVK resource.
			// Deletion path surfaces errors directly (no backoff semantics); the
			// controller-runtime workqueue handles retry via its own rate limiter.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).To(HaveOccurred(), "deletion partial failure surfaces error")

			// Verify finalizer is still present (not removed due to partial failure).
			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&updated, opmreconcile.FinalizerName)).To(BeTrue())

			// Cleanup: remove finalizer manually so the object can be deleted.
			controllerutil.RemoveFinalizer(&updated, opmreconcile.FinalizerName)
			Expect(k8sClient.Update(ctx, &updated)).To(Succeed())
		})
	})

	Context("Failure counters", func() {
		It("should increment reconcile counter on failed reconcile", func() {
			ctx := context.Background()

			// ModuleRelease points to a non-existent source → FailedStalled.
			createModuleRelease(ctx, "counter-fail-mr")

			reconciler := &ModuleReleaseReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: record.NewFakeRecorder(10),
				Renderer:      resolutionErrorRenderer(),
			}

			nn := types.NamespacedName{Name: "counter-fail-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile fails (source not found → FailedStalled).
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred()) // FailedStalled returns nil error

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(1)))

			// Third reconcile increments again.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(2)))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "counter-fail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should increment apply counter on apply failure", func() {
			ctx := context.Background()

			createModuleRelease(ctx, "apply-fail-mr")

			nn := types.NamespacedName{Name: "apply-fail-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			realReconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}
			_, err := realReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// ResourceManager with a client that fails all Patch calls (SSA apply).
			realWithWatch, watchErr := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
			Expect(watchErr).NotTo(HaveOccurred())
			failingClient := interceptor.NewClient(realWithWatch, interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("injected apply failure")
				},
			})

			failReconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(failingClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// Second reconcile — drift detection fails (non-blocking), apply fails.
			result, err := failReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "transient failure returns nil error with backoff")
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "transient failure requeues with backoff")

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Apply).To(Equal(int64(1)))
			Expect(mr.Status.FailureCounters.Drift).To(Equal(int64(1)))
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(1)))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "apply-fail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should increment prune counter on prune failure", func() {
			ctx := context.Background()

			// Create MR with prune enabled.
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-fail-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune: true,
					Module: releasesv1alpha1.ModuleReference{
						Path:    "opmodel.dev/test/module",
						Version: "v0.1.0",
					},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			nn := types.NamespacedName{Name: "prune-fail-mr", Namespace: namespace}

			realReconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// Finalizer reconcile.
			_, err := realReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Full reconcile — applies resources, creates inventory.
			_, err = realReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Add a fake stale entry to the inventory.
			Expect(k8sClient.Get(ctx, nn, mr)).To(Succeed())
			mr.Status.Inventory.Entries = append(mr.Status.Inventory.Entries,
				releasesv1alpha1.InventoryEntry{
					Version: "v1", Kind: "ConfigMap",
					Namespace: namespace, Name: "stale-cm",
				})
			Expect(k8sClient.Status().Update(ctx, mr)).To(Succeed())

			// Change values to avoid no-op detection.
			Expect(k8sClient.Get(ctx, nn, mr)).To(Succeed())
			mr.Spec.Values.Raw = []byte(`{"message": "world"}`)
			Expect(k8sClient.Update(ctx, mr)).To(Succeed())

			// Reconciler with Delete interceptor that fails for the stale resource.
			realWithWatch2, watchErr := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
			Expect(watchErr).NotTo(HaveOccurred())
			failingDeleteClient := interceptor.NewClient(realWithWatch2, interceptor.Funcs{
				Delete: func(_ context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if obj.GetName() == "stale-cm" {
						return fmt.Errorf("injected delete failure")
					}
					return c.Delete(ctx, obj, opts...)
				},
			})

			pruneFailReconciler := &ModuleReleaseReconciler{
				Client:          failingDeleteClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// Third reconcile — apply succeeds, prune fails.
			result, err := pruneFailReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "transient failure returns nil error with backoff")
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "transient failure requeues with backoff")

			Expect(k8sClient.Get(ctx, nn, mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Prune).To(Equal(int64(1)))
			Expect(mr.Status.FailureCounters.Apply).To(Equal(int64(0)))
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(1)))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "prune-fail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should reset counters on successful reconcile", func() {
			ctx := context.Background()

			createModuleRelease(ctx, "counter-reset-mr")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "counter-reset-mr", Namespace: namespace}

			// Finalizer reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Pre-seed failure counters to simulate prior failures.
			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			mr.Status.FailureCounters = &releasesv1alpha1.FailureCounters{
				Reconcile: 5,
				Apply:     3,
				Prune:     2,
				Drift:     1,
			}
			Expect(k8sClient.Status().Update(ctx, &mr)).To(Succeed())

			// Full reconcile — succeeds and should reset counters.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(0)))
			Expect(mr.Status.FailureCounters.Apply).To(Equal(int64(0)))
			Expect(mr.Status.FailureCounters.Prune).To(Equal(int64(0)))
			Expect(mr.Status.FailureCounters.Drift).To(Equal(int64(0)))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "counter-reset-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Event emission", func() {
		It("should emit Applied and ReconciliationSucceeded events after successful reconcile", func() {
			ctx := context.Background()

			createModuleRelease(ctx, "event-apply-mr")

			recorder := record.NewFakeRecorder(10)
			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   recorder,
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "event-apply-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile runs the full pipeline.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify Applied event with resource counts.
			var appliedEvent string
			Eventually(recorder.Events).Should(Receive(&appliedEvent))
			Expect(appliedEvent).To(ContainSubstring("Applied"))
			Expect(appliedEvent).To(ContainSubstring("created"))
			Expect(appliedEvent).To(ContainSubstring("unchanged"))

			// Verify ReconciliationSucceeded event.
			var succeededEvent string
			Eventually(recorder.Events).Should(Receive(&succeededEvent))
			Expect(succeededEvent).To(ContainSubstring("ReconciliationSucceeded"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "event-apply-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should emit Warning event on apply failure", func() {
			ctx := context.Background()

			createModuleRelease(ctx, "event-applyfail-mr")

			nn := types.NamespacedName{Name: "event-applyfail-mr", Namespace: namespace}

			// First reconcile adds finalizer (use real reconciler).
			realReconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}
			_, err := realReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Build a failing ResourceManager.
			realWithWatch, watchErr := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
			Expect(watchErr).NotTo(HaveOccurred())
			failingClient := interceptor.NewClient(realWithWatch, interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("injected apply failure")
				},
			})

			recorder := record.NewFakeRecorder(10)
			failReconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(failingClient, "opm-controller"),
				EventRecorder:   recorder,
				Renderer:        &stubRenderer{},
			}

			// Second reconcile — apply fails.
			result, err := failReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "transient failure returns nil error with backoff")
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "transient failure requeues with backoff")

			// Verify Warning/ApplyFailed event.
			var event string
			Eventually(recorder.Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring("ApplyFailed"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "event-applyfail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should emit Suspended event when reconcile is suspended", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-suspend-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			recorder := record.NewFakeRecorder(10)
			reconciler := &ModuleReleaseReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),

				EventRecorder: recorder,
				Renderer:      &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "event-suspend-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile hits suspend.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify Normal/Suspended event.
			var event string
			Eventually(recorder.Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Normal"))
			Expect(event).To(ContainSubstring("Suspended"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, mr)).To(Succeed())
		})

		It("should emit Resumed event when unsuspended", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-resume-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values:  &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "event-resume-mr", Namespace: namespace}

			// Finalizer reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Suspend reconcile.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Unsuspend.
			var current releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = false
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Fresh recorder for the resume reconcile.
			resumeRecorder := record.NewFakeRecorder(10)
			reconciler.EventRecorder = resumeRecorder

			// Resume reconcile — should emit Resumed, then Applied, then ReconciliationSucceeded.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// First event should be Resumed.
			var event string
			Eventually(resumeRecorder.Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Normal"))
			Expect(event).To(ContainSubstring("Resumed"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "event-resume-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should emit NoOp event when digests match", func() {
			ctx := context.Background()

			createModuleRelease(ctx, "event-noop-mr")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   record.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "event-noop-mr", Namespace: namespace}

			// Finalizer reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// First full reconcile — applies resources.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Fresh recorder for the no-op reconcile.
			noopRecorder := record.NewFakeRecorder(10)
			reconciler.EventRecorder = noopRecorder

			// Second full reconcile — should be no-op.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify Normal/NoOp event.
			var event string
			Eventually(noopRecorder.Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Normal"))
			Expect(event).To(ContainSubstring("NoOp"))
			Expect(event).To(ContainSubstring("No changes detected"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "event-noop-mr", Namespace: namespace},
			})).To(Succeed())
		})

	})
})
