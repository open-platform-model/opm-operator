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
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	oerrors "github.com/open-platform-model/library/opm/errors"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

var _ = Describe("ModuleInstance Reconcile Loop", func() {
	const namespace = "default"

	createModuleInstance := func(ctx context.Context, name string) *releasesv1alpha1.ModuleInstance {
		mr := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
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

			createModuleInstance(ctx, "full-reconcile-mr")

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "full-reconcile-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

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
			var mr releasesv1alpha1.ModuleInstance
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "full-reconcile-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Suspend check", func() {
		It("should skip reconciliation when suspend is true and set correct conditions", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspended-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Renderer:      &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "suspended-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

			// Second reconcile hits suspend.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify conditions: Ready=False/Suspended, Reconciling removed, Stalled removed.
			var updated releasesv1alpha1.ModuleInstance
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

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspend-preserve-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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
			var beforeSuspend releasesv1alpha1.ModuleInstance
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
			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = true
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Reconcile while suspended.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify status is preserved.
			var afterSuspend releasesv1alpha1.ModuleInstance
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "suspend-preserve-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should perform full reconcile when unsuspended", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "resume-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values:  &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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
			var suspended releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &suspended)).To(Succeed())
			ready := apimeta.FindStatusCondition(suspended.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(status.SuspendedReason))

			// Unsuspend.
			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = false
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Reconcile after unsuspend — should perform full reconcile.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify full reconcile happened: Ready=True, resources applied.
			var resumed releasesv1alpha1.ModuleInstance
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "resume-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("CLI-owned instance (owner marker)", func() {
		It("should skip reconciliation, add no finalizer, and acknowledge with ManagedExternally", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cli-owned-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  releasesv1alpha1.OwnerCLI,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "cli-owned-mr", Namespace: namespace}

			// A single reconcile: no finalizer round-trip, straight to the skip gate.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			var updated releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			// No finalizer added.
			Expect(controllerutil.ContainsFinalizer(&updated, opmreconcile.FinalizerName)).To(BeFalse())

			// Ready=Unknown/ManagedExternally; Reconciling and Stalled absent.
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionUnknown))
			Expect(ready.Reason).To(Equal(status.ManagedExternallyReason))
			Expect(apimeta.FindStatusCondition(updated.Status.Conditions, status.ReconcilingCondition)).To(BeNil())
			Expect(apimeta.FindStatusCondition(updated.Status.Conditions, status.StalledCondition)).To(BeNil())

			// observedGeneration not stamped — no reconcile happened.
			Expect(updated.Status.ObservedGeneration).To(BeZero())

			// No resources applied.
			var cm corev1.ConfigMap
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			// Cleanup (no finalizer to clear).
			Expect(k8sClient.Delete(ctx, &updated)).To(Succeed())
		})

		It("should leave CLI-written status untouched and re-acknowledge idempotently", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cli-owned-status-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  releasesv1alpha1.OwnerCLI,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())
			nn := types.NamespacedName{Name: "cli-owned-status-mr", Namespace: namespace}

			// Simulate the CLI writing its own inventory + lastApplied* digests.
			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Status.InstanceUUID = "cli-uuid-123"
			current.Status.LastAppliedSourceDigest = "sha256:clisource"
			current.Status.LastAppliedConfigDigest = "sha256:cliconfig"
			current.Status.LastAppliedRenderDigest = "sha256:clirender"
			current.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: 7,
				Digest:   "sha256:cliinv",
				Count:    1,
				Entries: []releasesv1alpha1.InventoryEntry{
					{Kind: "ConfigMap", Name: "cli-managed", Namespace: namespace},
				},
			}
			Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// CLI-written status survives untouched (0006:D25 boundary).
			var afterAck releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &afterAck)).To(Succeed())
			Expect(afterAck.Status.InstanceUUID).To(Equal("cli-uuid-123"))
			Expect(afterAck.Status.LastAppliedSourceDigest).To(Equal("sha256:clisource"))
			Expect(afterAck.Status.LastAppliedConfigDigest).To(Equal("sha256:cliconfig"))
			Expect(afterAck.Status.LastAppliedRenderDigest).To(Equal("sha256:clirender"))
			Expect(afterAck.Status.Inventory).NotTo(BeNil())
			Expect(afterAck.Status.Inventory.Revision).To(Equal(int64(7)))
			Expect(afterAck.Status.Inventory.Digest).To(Equal("sha256:cliinv"))
			Expect(afterAck.Status.Inventory.Entries).To(HaveLen(1))
			Expect(afterAck.Status.Inventory.Entries[0].Name).To(Equal("cli-managed"))

			// Capture the acknowledgement transition time.
			ackReady := apimeta.FindStatusCondition(afterAck.Status.Conditions, status.ReadyCondition)
			Expect(ackReady).NotTo(BeNil())
			Expect(ackReady.Reason).To(Equal(status.ManagedExternallyReason))
			firstTransition := ackReady.LastTransitionTime

			// Re-reconcile (e.g. a Platform-watch re-enqueue) is a no-op: the
			// condition does not transition again and CLI status is still intact.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var afterReAck releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &afterReAck)).To(Succeed())
			reReady := apimeta.FindStatusCondition(afterReAck.Status.Conditions, status.ReadyCondition)
			Expect(reReady).NotTo(BeNil())
			Expect(reReady.Reason).To(Equal(status.ManagedExternallyReason))
			Expect(reReady.LastTransitionTime).To(Equal(firstTransition))
			Expect(afterReAck.Status.Inventory.Digest).To(Equal("sha256:cliinv"))

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &afterReAck)).To(Succeed())
		})

		It("should not block deletion of a CLI-owned instance and should prune nothing", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cli-owned-delete-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  releasesv1alpha1.OwnerCLI,
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())
			nn := types.NamespacedName{Name: "cli-owned-delete-mr", Namespace: namespace}

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// Acknowledge it once (no finalizer added).
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			var acked releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &acked)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&acked, opmreconcile.FinalizerName)).To(BeFalse())

			// Delete: with no finalizer the object is removed immediately.
			Expect(k8sClient.Delete(ctx, &acked)).To(Succeed())

			// A reconcile of the deleting (now gone) instance is a clean no-op.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Object is gone — deletion was never blocked.
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleInstance
				return k8sClient.Get(ctx, nn, &deleted) != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

		It("should adopt the instance on handoff when owner flips to operator", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cli-handoff-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  releasesv1alpha1.OwnerCLI,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())
			nn := types.NamespacedName{Name: "cli-handoff-mr", Namespace: namespace}

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// CLI-owned: acknowledged, no finalizer, no resources.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			var acked releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &acked)).To(Succeed())
			ackReady := apimeta.FindStatusCondition(acked.Status.Conditions, status.ReadyCondition)
			Expect(ackReady).NotTo(BeNil())
			Expect(ackReady.Reason).To(Equal(status.ManagedExternallyReason))
			Expect(controllerutil.ContainsFinalizer(&acked, opmreconcile.FinalizerName)).To(BeFalse())

			// Flip owner to operator (the handoff).
			acked.Spec.Owner = releasesv1alpha1.OwnerOperator
			Expect(k8sClient.Update(ctx, &acked)).To(Succeed())

			// Next reconcile falls through to the normal path: adds the finalizer.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

			var adopted releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &adopted)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&adopted, opmreconcile.FinalizerName)).To(BeTrue())

			// Full reconcile: renders, applies, overwrites ManagedExternally with the real Ready.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var reconciled releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &reconciled)).To(Succeed())
			ready := apimeta.FindStatusCondition(reconciled.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).NotTo(Equal(status.ManagedExternallyReason))
			Expect(reconciled.Status.Inventory).NotTo(BeNil())

			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Get(ctx, nn, &reconciled)).To(Succeed())
			controllerutil.RemoveFinalizer(&reconciled, opmreconcile.FinalizerName)
			Expect(k8sClient.Update(ctx, &reconciled)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &reconciled)).To(Succeed())
		})
	})

	Context("Operator-managed default (empty owner)", func() {
		It("should reconcile normally when spec.owner is absent (no skip, no ManagedExternally)", func() {
			ctx := context.Background()

			// createModuleInstance sets no spec.owner — the operator-managed
			// default the 0006:D24 skew model leans on.
			createModuleInstance(ctx, "empty-owner-mr")

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "empty-owner-mr", Namespace: namespace}

			// First reconcile registers the cleanup finalizer — not the
			// CLI-owned single-pass acknowledgement.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

			var afterFinalizer releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &afterFinalizer)).To(Succeed())
			Expect(afterFinalizer.Spec.Owner).To(BeEmpty())
			Expect(controllerutil.ContainsFinalizer(&afterFinalizer, opmreconcile.FinalizerName)).To(BeTrue())

			// Second reconcile runs the full render/apply pipeline.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// The stub-rendered ConfigMap was applied.
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())
			Expect(cm.Data["message"]).To(Equal("hello"))

			// Ready=True with a real reconcile reason — never ManagedExternally.
			var reconciled releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &reconciled)).To(Succeed())
			ready := apimeta.FindStatusCondition(reconciled.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).NotTo(Equal(status.ManagedExternallyReason))
			Expect(reconciled.Status.ObservedGeneration).To(Equal(reconciled.Generation))

			// Cleanup.
			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Get(ctx, nn, &reconciled)).To(Succeed())
			controllerutil.RemoveFinalizer(&reconciled, opmreconcile.FinalizerName)
			Expect(k8sClient.Update(ctx, &reconciled)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &reconciled)).To(Succeed())
		})

		It("should reject unknown owner values at the API server (enum validation)", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unknown-owner-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  releasesv1alpha1.OwnerType("future-actor"),
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
				},
			}

			err := k8sClient.Create(ctx, mr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unsupported value"))
			Expect(err.Error()).To(ContainSubstring("future-actor"))
		})
	})

	Context("Operator's own instance", func() {
		// ownModulePath is the module path signal: it makes an instance the
		// operator's own in any namespace, so most specs stay in "default".
		const ownModulePath = "opmodel.dev/modules/opm_operator@v0"

		newOwnInstance := func(name string, owner releasesv1alpha1.OwnerType) *releasesv1alpha1.ModuleInstance {
			return &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  owner,
					Module: releasesv1alpha1.ModuleReference{Path: ownModulePath, Version: "v0.1.0"},
				},
			}
		}

		newReconciler := func(recorder *events.FakeRecorder) *ModuleInstanceReconciler {
			return &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   recorder,
				Renderer:        failOnRenderRenderer{},
			}
		}

		expectRefused := func(mi *releasesv1alpha1.ModuleInstance) {
			GinkgoHelper()
			Expect(controllerutil.ContainsFinalizer(mi, opmreconcile.FinalizerName)).To(BeFalse())
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.SelfManagementRefusedReason))
			Expect(ready.Message).To(ContainSubstring("Set spec.owner to cli"))
			stalled := apimeta.FindStatusCondition(mi.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.SelfManagementRefusedReason))
			Expect(apimeta.FindStatusCondition(mi.Status.Conditions, status.ReconcilingCondition)).To(BeNil())
			Expect(mi.Status.ObservedGeneration).To(Equal(mi.Generation))
		}

		It("refuses an own instance flipped to operator without adding the finalizer", func() {
			ctx := context.Background()
			mi := newOwnInstance("own-operator-mi", releasesv1alpha1.OwnerOperator)
			mi.Spec.Prune = true
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			recorder := events.NewFakeRecorder(10)
			result, err := newReconciler(recorder).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			var refused releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &refused)).To(Succeed())
			expectRefused(&refused)
			Expect(apimeta.FindStatusCondition(refused.Status.Conditions, status.ReadyCondition).Message).
				To(ContainSubstring("module opmodel.dev/modules/opm_operator"))
			Expect(refused.Status.Inventory).To(BeNil())
			Expect(refused.Status.InstanceUUID).To(BeEmpty())
			Expect(refused.Status.LastAttemptedAction).To(BeEmpty())
			Expect(refused.Status.LastAttemptedAt).To(BeNil())
			Expect(refused.Status.History).To(BeEmpty())
			Expect(refused.Status.FailureCounters).To(BeNil())

			var event string
			Eventually(recorder.Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring(status.SelfManagementRefusedReason))

			var cm corev1.ConfigMap
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)
			Expect(client.IgnoreNotFound(err)).To(Succeed())
			Expect(err).To(HaveOccurred())

			Expect(k8sClient.Delete(ctx, &refused)).To(Succeed())
		})

		It("refuses an own instance with no owner at the fixed coordinates", func() {
			ctx := context.Background()
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "opm-operator-system"}}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())

			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "opm-operator", Namespace: "opm-operator-system"},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "example.com/forks/operator@v0", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			_, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var refused releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &refused)).To(Succeed())
			Expect(refused.Spec.Owner).To(BeEmpty())
			expectRefused(&refused)
			Expect(apimeta.FindStatusCondition(refused.Status.Conditions, status.ReadyCondition).Message).
				To(ContainSubstring("name opm-operator in namespace opm-operator-system"))

			Expect(k8sClient.Delete(ctx, &refused)).To(Succeed())
		})

		It("refuses a suspended own instance instead of marking it Suspended", func() {
			ctx := context.Background()
			mi := newOwnInstance("own-suspended-mi", releasesv1alpha1.OwnerOperator)
			mi.Spec.Suspend = true
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			_, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var refused releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &refused)).To(Succeed())
			expectRefused(&refused)

			Expect(k8sClient.Delete(ctx, &refused)).To(Succeed())
		})

		It("refuses an instance whose recorded inventory holds an operator CRD", func() {
			ctx := context.Background()
			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "renamed-fork-mi", Namespace: namespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  releasesv1alpha1.OwnerOperator,
					Module: releasesv1alpha1.ModuleReference{Path: "example.com/forks/operator@v0", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: 1,
				Count:    1,
				Entries: []releasesv1alpha1.InventoryEntry{{
					Group:   "apiextensions.k8s.io",
					Kind:    "CustomResourceDefinition",
					Name:    "moduleinstances.opmodel.dev",
					Version: "v1",
				}},
			}
			Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())

			_, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var refused releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &refused)).To(Succeed())
			expectRefused(&refused)
			Expect(apimeta.FindStatusCondition(refused.Status.Conditions, status.ReadyCondition).Message).
				To(ContainSubstring("inventory records CustomResourceDefinition moduleinstances.opmodel.dev"))
			Expect(refused.Status.Inventory.Revision).To(Equal(int64(1)))

			Expect(k8sClient.Delete(ctx, &refused)).To(Succeed())
		})

		It("removes conditions left by an earlier adoption and leaves CLI-written status alone", func() {
			ctx := context.Background()
			mi := newOwnInstance("own-adopted-mi", releasesv1alpha1.OwnerOperator)
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			status.MarkModuleResolved(&current, ownModulePath)
			status.MarkDrifted(&current, 1)
			current.Status.InstanceUUID = "cli-uuid-own"
			current.Status.LastAppliedSourceDigest = "sha256:clisource"
			current.Status.LastAppliedConfigDigest = "sha256:cliconfig"
			current.Status.LastAppliedRenderDigest = "sha256:clirender"
			current.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: 3,
				Digest:   "sha256:cliinv",
				Count:    1,
				Entries: []releasesv1alpha1.InventoryEntry{
					{Kind: "ServiceAccount", Name: "opm-operator-controller-manager", Namespace: namespace, Version: "v1"},
				},
			}
			Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())

			_, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var refused releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &refused)).To(Succeed())
			expectRefused(&refused)
			Expect(apimeta.FindStatusCondition(refused.Status.Conditions, status.ModuleResolvedCondition)).To(BeNil())
			Expect(apimeta.FindStatusCondition(refused.Status.Conditions, status.DriftedCondition)).To(BeNil())
			Expect(refused.Status.InstanceUUID).To(Equal("cli-uuid-own"))
			Expect(refused.Status.LastAppliedSourceDigest).To(Equal("sha256:clisource"))
			Expect(refused.Status.LastAppliedConfigDigest).To(Equal("sha256:cliconfig"))
			Expect(refused.Status.LastAppliedRenderDigest).To(Equal("sha256:clirender"))
			Expect(refused.Status.Inventory).NotTo(BeNil())
			Expect(refused.Status.Inventory.Revision).To(Equal(int64(3)))
			Expect(refused.Status.Inventory.Digest).To(Equal("sha256:cliinv"))
			Expect(refused.Status.Inventory.Entries).To(HaveLen(1))

			Expect(k8sClient.Delete(ctx, &refused)).To(Succeed())
		})

		It("re-refuses with an empty patch and no event, and hands back to the CLI cleanly", func() {
			ctx := context.Background()
			mi := newOwnInstance("own-rerefuse-mi", releasesv1alpha1.OwnerOperator)
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			recorder := events.NewFakeRecorder(10)
			reconciler := newReconciler(recorder)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Eventually(recorder.Events).Should(Receive())

			var first releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &first)).To(Succeed())
			expectRefused(&first)
			firstReady := apimeta.FindStatusCondition(first.Status.Conditions, status.ReadyCondition)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Consistently(recorder.Events, 200*time.Millisecond).ShouldNot(Receive())

			var second releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &second)).To(Succeed())
			Expect(second.ResourceVersion).To(Equal(first.ResourceVersion))
			Expect(apimeta.FindStatusCondition(second.Status.Conditions, status.ReadyCondition).LastTransitionTime).
				To(Equal(firstReady.LastTransitionTime))

			// Hand the instance back to the CLI: the owner-skip gate acknowledges
			// it and the refusal's Stalled goes away.
			second.Spec.Owner = releasesv1alpha1.OwnerCLI
			Expect(k8sClient.Update(ctx, &second)).To(Succeed())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var handedBack releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &handedBack)).To(Succeed())
			ready := apimeta.FindStatusCondition(handedBack.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionUnknown))
			Expect(ready.Reason).To(Equal(status.ManagedExternallyReason))
			Expect(apimeta.FindStatusCondition(handedBack.Status.Conditions, status.StalledCondition)).To(BeNil())

			Expect(k8sClient.Delete(ctx, &handedBack)).To(Succeed())
		})

		It("releases a leftover finalizer from a live own instance and refuses it", func() {
			ctx := context.Background()
			mi := newOwnInstance("own-leftover-mi", releasesv1alpha1.OwnerOperator)
			mi.Finalizers = []string{opmreconcile.FinalizerName}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			_, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var refused releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &refused)).To(Succeed())
			expectRefused(&refused)

			Expect(k8sClient.Delete(ctx, &refused)).To(Succeed())
		})

		It("releases a leftover finalizer from a deleting own instance and prunes nothing", func() {
			ctx := context.Background()

			// OPM-managed, so the normal deletion path would prune it.
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "own-deleting-kept",
					Namespace: namespace,
					Labels:    map[string]string{core.LabelManagedBy: core.LabelManagedByControllerValue},
				},
				Data: map[string]string{"k": "v"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			mi := newOwnInstance("own-deleting-mi", releasesv1alpha1.OwnerOperator)
			mi.Spec.Prune = true
			mi.Finalizers = []string{opmreconcile.FinalizerName}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Status.Inventory = &releasesv1alpha1.Inventory{
				Revision: 1,
				Count:    1,
				Entries: []releasesv1alpha1.InventoryEntry{
					{Kind: "ConfigMap", Name: "own-deleting-kept", Namespace: namespace, Version: "v1"},
				},
			}
			Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &current)).To(Succeed())

			result, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Eventually(func() bool {
				var gone releasesv1alpha1.ModuleInstance
				return k8sClient.Get(ctx, nn, &gone) != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			var kept corev1.ConfigMap
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &kept)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &kept)).To(Succeed())
		})

		It("leaves a CLI-owned own instance alone, leftover finalizer included", func() {
			ctx := context.Background()
			mi := newOwnInstance("own-cli-mi", releasesv1alpha1.OwnerCLI)
			mi.Finalizers = []string{opmreconcile.FinalizerName}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			_, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var acked releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &acked)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&acked, opmreconcile.FinalizerName)).To(BeTrue())
			ready := apimeta.FindStatusCondition(acked.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(status.ManagedExternallyReason))

			controllerutil.RemoveFinalizer(&acked, opmreconcile.FinalizerName)
			Expect(k8sClient.Update(ctx, &acked)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &acked)).To(Succeed())
		})

		It("refuses the operator's own instance when the CLI hands it to the operator", func() {
			ctx := context.Background()
			mi := newOwnInstance("own-handoff-mi", releasesv1alpha1.OwnerCLI)
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			reconciler := newReconciler(events.NewFakeRecorder(10))
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var acked releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &acked)).To(Succeed())
			Expect(apimeta.FindStatusCondition(acked.Status.Conditions, status.ReadyCondition).Reason).
				To(Equal(status.ManagedExternallyReason))

			acked.Spec.Owner = releasesv1alpha1.OwnerOperator
			Expect(k8sClient.Update(ctx, &acked)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			var refused releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &refused)).To(Succeed())
			expectRefused(&refused)

			Expect(k8sClient.Delete(ctx, &refused)).To(Succeed())
		})

		It("still registers the finalizer on an instance with only similar names", func() {
			ctx := context.Background()
			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "opm-operator", Namespace: namespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Owner:  releasesv1alpha1.OwnerOperator,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/modules/opm_operator_dashboard@v0", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := client.ObjectKeyFromObject(mi)

			result, err := newReconciler(events.NewFakeRecorder(10)).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

			var registered releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &registered)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&registered, opmreconcile.FinalizerName)).To(BeTrue())

			controllerutil.RemoveFinalizer(&registered, opmreconcile.FinalizerName)
			Expect(k8sClient.Update(ctx, &registered)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &registered)).To(Succeed())
		})
	})

	Context("No-op detection", func() {
		It("should skip apply on second reconcile when digests match", func() {
			ctx := context.Background()

			createModuleInstance(ctx, "noop-mr")

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "noop-mr", Namespace: namespace}

			// Finalizer reconcile.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

			// First full reconcile — applies resources.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify first reconcile applied.
			var mr releasesv1alpha1.ModuleInstance
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "noop-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Last applied version", func() {
		newReconciler := func(renderer *stubRenderer) *ModuleInstanceReconciler {
			return &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        renderer,
			}
		}

		// appliedInstance creates an instance and reconciles it to a first
		// successful apply with the default stub (version stubModuleVersion).
		appliedInstance := func(ctx context.Context, name string) types.NamespacedName {
			createModuleInstance(ctx, name)
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			r := newReconciler(&stubRenderer{})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			return nn
		}

		// changedRender is a render whose resources differ from the default
		// stub's, so its render digest differs and the reconcile applies.
		changedRender := func(version string) *render.RenderResult {
			values := &releasesv1alpha1.RawValues{}
			values.Raw = []byte(`{"message": "changed"}`)
			res := stubRenderResult(namespace, values)
			res.ModuleVersion = version
			return res
		}

		setRecordedVersion := func(ctx context.Context, nn types.NamespacedName, version string) {
			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			mi.Status.LastAppliedVersion = version
			Expect(k8sClient.Status().Update(ctx, &mi)).To(Succeed())
		}

		cleanup := func(ctx context.Context, nn types.NamespacedName) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			}))).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: namespace},
			})).To(Succeed())
		}

		It("records the rendered module version on a successful apply", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "version-applied-mi")

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			Expect(mi.Status.LastAppliedVersion).To(Equal(stubModuleVersion))
			Expect(mi.Status.LastAppliedSourceDigest).NotTo(BeEmpty())

			cleanup(ctx, nn)
		})

		It("keeps the recorded version and digests when an apply fails", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "version-failed-mi")

			var before releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())

			realWithWatch, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
			Expect(err).NotTo(HaveOccurred())
			failingClient := interceptor.NewClient(realWithWatch, interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("injected apply failure")
				},
			})
			r := newReconciler(&stubRenderer{result: changedRender("0.2.0")})
			r.ResourceManager = apply.NewResourceManager(failingClient, "opm-controller")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse), "the apply failed")
			Expect(mi.Status.LastAppliedVersion).To(Equal(stubModuleVersion))
			Expect(mi.Status.LastAppliedSourceDigest).To(Equal(before.Status.LastAppliedSourceDigest))
			Expect(mi.Status.LastAppliedConfigDigest).To(Equal(before.Status.LastAppliedConfigDigest))
			Expect(mi.Status.LastAppliedRenderDigest).To(Equal(before.Status.LastAppliedRenderDigest))

			cleanup(ctx, nn)
		})

		It("fills an empty field on a NoOp without touching the attempt record", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "version-noop-fill-mi")
			setRecordedVersion(ctx, nn, "")

			var before releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
			Expect(before.Status.LastAppliedVersion).To(BeEmpty())

			_, err := newReconciler(&stubRenderer{}).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			Expect(mi.Status.LastAppliedVersion).To(Equal(stubModuleVersion))
			Expect(mi.Status.History).To(HaveLen(len(before.Status.History)), "a NoOp records no history")
			Expect(mi.Status.LastAttemptedAt).To(Equal(before.Status.LastAttemptedAt), "a NoOp is not an attempt")

			cleanup(ctx, nn)
		})

		It("corrects a stale version on a NoOp after an ownership handback", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "version-noop-stale-mi")
			setRecordedVersion(ctx, nn, "9.9.9")

			var before releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())

			_, err := newReconciler(&stubRenderer{}).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			Expect(mi.Status.LastAppliedVersion).To(Equal(stubModuleVersion),
				"a NoOp re-proves the applied version, so it replaces a stale one")
			Expect(mi.Status.History).To(HaveLen(len(before.Status.History)), "the reconcile was a NoOp")

			cleanup(ctx, nn)
		})

		It("clears the field on an apply whose render reports no version", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "version-cleared-mi")

			_, err := newReconciler(&stubRenderer{result: changedRender("")}).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue), "the apply succeeded")
			Expect(mi.Status.LastAppliedRenderDigest).NotTo(BeEmpty())

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(releasesv1alpha1.GroupVersion.WithKind("ModuleInstance"))
			Expect(k8sClient.Get(ctx, nn, obj)).To(Succeed())
			_, found, err := unstructured.NestedString(obj.Object, "status", "lastAppliedVersion")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse(), "a stale version must not survive the apply")

			cleanup(ctx, nn)
		})
	})

	Context("Last applied inputs", func() {
		newReconciler := func(renderer *stubRenderer, interval time.Duration) *ModuleInstanceReconciler {
			return &ModuleInstanceReconciler{
				Client:              k8sClient,
				Scheme:              k8sClient.Scheme(),
				ResourceManager:     apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:       events.NewFakeRecorder(10),
				Renderer:            renderer,
				OperatorVersion:     testOperatorVersion,
				LibraryVersion:      testLibraryVersion,
				DriftRenderInterval: interval,
			}
		}

		// The reconcilers here run with no Platform, so the pre-render key is
		// incomplete and no reconcile skips; they observe what is recorded.
		appliedInstance := func(ctx context.Context, name string) types.NamespacedName {
			createModuleInstance(ctx, name)
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			r := newReconciler(&stubRenderer{}, 30*time.Minute)
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			return nn
		}

		changedRender := func(identity string) *render.RenderResult {
			values := &releasesv1alpha1.RawValues{}
			values.Raw = []byte(`{"message": "changed"}`)
			res := stubRenderResult(namespace, values)
			res.PlatformIdentity = identity
			return res
		}

		clearRecorded := func(ctx context.Context, nn types.NamespacedName) {
			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			mi.Status.LastAppliedInputs = nil
			Expect(k8sClient.Status().Update(ctx, &mi)).To(Succeed())
		}

		cleanup := func(ctx context.Context, nn types.NamespacedName) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			}))).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: namespace},
			})).To(Succeed())
		}

		It("records the key and the render time on the first successful apply", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "inputs-applied-mi")

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			Expect(mi.Status.LastAppliedInputs).NotTo(BeNil())
			Expect(mi.Status.LastAppliedInputs.Digest).To(HavePrefix("sha256:"))
			Expect(mi.Status.LastAppliedInputs.RenderedAt.IsZero()).To(BeFalse())
			want := status.RenderInputKey{
				Source:          mi.Status.LastAppliedSourceDigest,
				Config:          mi.Status.LastAppliedConfigDigest,
				PackageIdentity: stubPlatformIdentity,
				SkewPolicy:      stubSkewPolicy,
				OperatorVersion: testOperatorVersion,
				LibraryVersion:  testLibraryVersion,
			}
			Expect(mi.Status.LastAppliedInputs.Digest).To(Equal(want.Digest()),
				"the key is built from the render's own platform report")

			cleanup(ctx, nn)
		})

		It("keeps the key when an apply with changed values fails", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "inputs-failed-mi")

			var before releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
			Expect(before.Status.LastAppliedInputs).NotTo(BeNil())

			Eventually(func() error {
				var latest releasesv1alpha1.ModuleInstance
				if err := k8sClient.Get(ctx, nn, &latest); err != nil {
					return err
				}
				latest.Spec.Values.Raw = []byte(`{"message": "changed"}`)
				return k8sClient.Update(ctx, &latest)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			realWithWatch, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
			Expect(err).NotTo(HaveOccurred())
			failingClient := interceptor.NewClient(realWithWatch, interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("injected apply failure")
				},
			})
			r := newReconciler(&stubRenderer{}, 30*time.Minute)
			r.ResourceManager = apply.NewResourceManager(failingClient, "opm-controller")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse), "the apply failed")
			Expect(mi.Status.LastAppliedInputs).NotTo(BeNil())
			Expect(mi.Status.LastAppliedInputs.Digest).To(Equal(before.Status.LastAppliedInputs.Digest))
			Expect(mi.Status.LastAppliedInputs.RenderedAt.Equal(&before.Status.LastAppliedInputs.RenderedAt)).To(BeTrue())

			cleanup(ctx, nn)
		})

		It("rewrites the key on a NoOp without touching the attempt record", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "inputs-noop-mi")
			var applied releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &applied)).To(Succeed())
			clearRecorded(ctx, nn)

			var before releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
			Expect(before.Status.LastAppliedInputs).To(BeNil())

			_, err := newReconciler(&stubRenderer{}, 30*time.Minute).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			Expect(mi.Status.LastAppliedInputs).NotTo(BeNil())
			Expect(mi.Status.LastAppliedInputs.Digest).To(Equal(applied.Status.LastAppliedInputs.Digest))
			Expect(mi.Status.History).To(HaveLen(len(before.Status.History)), "a NoOp records no history")
			Expect(mi.Status.LastAttemptedAt).To(Equal(before.Status.LastAttemptedAt), "a NoOp is not an attempt")

			cleanup(ctx, nn)
		})

		It("leaves the field on a NoOp while the skip is disabled", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "inputs-noop-disabled-mi")
			clearRecorded(ctx, nn)

			_, err := newReconciler(&stubRenderer{}, 0).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			Expect(mi.Status.LastAppliedInputs).To(BeNil(), "with the skip disabled a NoOp records nothing")

			cleanup(ctx, nn)
		})

		It("clears the key on an apply whose render reports no platform", func() {
			ctx := context.Background()
			nn := appliedInstance(ctx, "inputs-cleared-mi")

			_, err := newReconciler(&stubRenderer{result: changedRender("")}, 30*time.Minute).
				Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue), "the apply succeeded")
			Expect(mi.Status.LastAppliedInputs).To(BeNil(), "an incomplete key must not leave a stale one behind")

			cleanup(ctx, nn)
		})
	})

	Context("Finalizer registration", func() {
		It("should add finalizer on first reconcile", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-add-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Renderer:      &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "finalizer-add-mr", Namespace: namespace}
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

			// Verify finalizer was added.
			var updated releasesv1alpha1.ModuleInstance
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

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-prune-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "delete-prune-mr", Namespace: namespace}

			// Finalizer reconcile.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))

			// Full reconcile — applies the ConfigMap.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap exists.
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-module", Namespace: namespace,
			}, &cm)).To(Succeed())

			// Delete the ModuleInstance (sets DeletionTimestamp, blocked by finalizer).
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
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

			// Verify ModuleInstance is gone (finalizer removed, deletion completed).
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleInstance
				err := k8sClient.Get(ctx, nn, &deleted)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		})
	})

	Context("Deletion with prune disabled", func() {
		It("should remove finalizer without deleting resources", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-orphan-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Prune:  false,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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

			// Delete the ModuleInstance.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
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

			// Verify ModuleInstance is gone.
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleInstance
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

			// Create a ModuleInstance with finalizer and fake inventory containing
			// a ConfigMap, a Namespace, and a CRD.
			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "delete-safety-mr",
					Namespace:  namespace,
					Finalizers: []string{opmreconcile.FinalizerName},
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			// Create a ConfigMap that's in the inventory. OPM managed-by label is
			// required for the prune ownership guard to permit deletion.
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "safety-test-cm",
					Namespace: namespace,
					Labels: map[string]string{
						core.LabelManagedBy: core.LabelManagedByControllerValue,
					},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			// Patch status with inventory that includes ConfigMap, Namespace, and CRD.
			var latest releasesv1alpha1.ModuleInstance
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

			// Delete the ModuleInstance.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-safety-mr", Namespace: namespace},
			})).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
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

			// ModuleInstance should be gone (finalizer removed).
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleInstance
				err := k8sClient.Get(ctx, nn, &deleted)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		})
	})

	Context("Deletion with suspend enabled", func() {
		It("should perform cleanup even when suspend is true", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-suspend-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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
			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = true
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Delete the ModuleInstance.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
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

			// Verify ModuleInstance is gone.
			Eventually(func() bool {
				var deleted releasesv1alpha1.ModuleInstance
				err := k8sClient.Get(ctx, nn, &deleted)
				return err != nil
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		})
	})

	Context("Deletion partial failure", func() {
		It("should retain finalizer when prune fails on some resources", func() {
			ctx := context.Background()

			// Create a ModuleInstance with finalizer and inventory containing
			// a resource with a non-existent GVK that will fail to delete.
			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "delete-partial-fail-mr",
					Namespace:  namespace,
					Finalizers: []string{opmreconcile.FinalizerName},
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Prune:  true,
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			// Patch status with inventory containing a resource that cannot be deleted
			// (non-existent GVK triggers a "no matches" error from the API server).
			nn := types.NamespacedName{Name: "delete-partial-fail-mr", Namespace: namespace}
			var latest releasesv1alpha1.ModuleInstance
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

			// Delete the ModuleInstance.
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-partial-fail-mr", Namespace: namespace},
			})).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Renderer:      &stubRenderer{},
			}

			// Reconcile should fail — prune cannot delete the non-existent GVK resource.
			// Deletion path surfaces errors directly (no backoff semantics); the
			// controller-runtime workqueue handles retry via its own rate limiter.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).To(HaveOccurred(), "deletion partial failure surfaces error")

			// Verify finalizer is still present (not removed due to partial failure).
			var updated releasesv1alpha1.ModuleInstance
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

			// The module cannot be acquired → FailedTransient (the counter
			// increments on every failed outcome, transient or stalled).
			createModuleInstance(ctx, "counter-fail-mr")

			reconciler := &ModuleInstanceReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Renderer:      acquireFailureRenderer(),
			}

			nn := types.NamespacedName{Name: "counter-fail-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile fails (module not acquired → FailedTransient).
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred()) // a classified failure returns nil error

			var mr releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(1)))

			// Third reconcile increments again.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(2)))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "counter-fail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should increment apply counter on apply failure", func() {
			ctx := context.Background()

			createModuleInstance(ctx, "apply-fail-mr")

			nn := types.NamespacedName{Name: "apply-fail-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			realReconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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

			failReconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(failingClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// Second reconcile — drift detection fails (non-blocking), apply fails.
			result, err := failReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "transient failure returns nil error with backoff")
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "transient failure requeues with backoff")

			var mr releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())
			Expect(mr.Status.FailureCounters).NotTo(BeNil())
			Expect(mr.Status.FailureCounters.Apply).To(Equal(int64(1)))
			Expect(mr.Status.FailureCounters.Drift).To(Equal(int64(1)))
			Expect(mr.Status.FailureCounters.Reconcile).To(Equal(int64(1)))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "apply-fail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should increment prune counter on prune failure", func() {
			ctx := context.Background()

			// Create MR with prune enabled.
			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-fail-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
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

			realReconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			// Finalizer reconcile.
			_, err := realReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Full reconcile — applies resources, creates inventory.
			_, err = realReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Pre-create the stale ConfigMap with OPM managed-by so the prune
			// ownership guard permits deletion; the interceptor will reject the
			// delete and drive the PruneFailed counter increment.
			staleCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stale-cm",
					Namespace: namespace,
					Labels: map[string]string{
						core.LabelManagedBy: core.LabelManagedByControllerValue,
					},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, staleCM)).To(Succeed())

			// Add a stale entry to the inventory.
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

			pruneFailReconciler := &ModuleInstanceReconciler{
				Client:          failingDeleteClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "prune-fail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should reset counters on successful reconcile", func() {
			ctx := context.Background()

			createModuleInstance(ctx, "counter-reset-mr")

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: "counter-reset-mr", Namespace: namespace}

			// Finalizer reconcile.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Pre-seed failure counters to simulate prior failures.
			var mr releasesv1alpha1.ModuleInstance
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "counter-reset-mr", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Acquisition failures", func() {
		// reconcileFailing adds the finalizer, then runs the failing reconcile
		// and returns its result, the stored instance and the events emitted.
		reconcileFailing := func(ctx context.Context, name string, renderer *stubRenderer) (
			reconcile.Result, releasesv1alpha1.ModuleInstance, []string,
		) {
			createModuleInstance(ctx, name)
			recorder := events.NewFakeRecorder(10)
			reconciler := &ModuleInstanceReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: recorder,
				Renderer:      renderer,
			}
			nn := types.NamespacedName{Name: name, Namespace: namespace}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "a classified render failure returns nil error")

			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())

			var es []string
			for len(recorder.Events) > 0 {
				es = append(es, <-recorder.Events)
			}
			return result, mi, es
		}

		deleteInstance := func(ctx context.Context, name string) {
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			})).To(Succeed())
		}

		It("retries an acquisition failure on the backoff instead of stalling", func() {
			ctx := context.Background()
			renderer := acquireFailureRenderer()
			before := time.Now()

			result, mi, es := reconcileFailing(ctx, "acquire-transient-mr", renderer)

			Expect(result.RequeueAfter).To(Equal(opmreconcile.ComputeBackoff(1)))
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.ResolutionFailedReason))
			Expect(ready.Message).To(Equal(renderer.err.Error()), "the status message is the acquisition error unchanged")
			Expect(apimeta.FindStatusCondition(mi.Status.Conditions, status.StalledCondition)).To(BeNil())
			Expect(mi.Status.NextRetryAt).NotTo(BeNil())
			Expect(mi.Status.NextRetryAt.Time).To(BeTemporally("~", before.Add(opmreconcile.ComputeBackoff(1)), 3*time.Second))
			Expect(es).To(ContainElement(SatisfyAll(
				HavePrefix(corev1.EventTypeWarning+" "+status.ResolutionFailedReason),
				ContainSubstring(renderer.err.Error()),
			)))

			deleteInstance(ctx, "acquire-transient-mr")
		})

		It("stalls an acquisition failure with a typed terminal cause", func() {
			ctx := context.Background()
			renderer := &stubRenderer{err: acquireErr(oerrors.IdentityError{
				Field:      "path",
				Declared:   "opmodel.dev/test/other",
				Fetched:    "opmodel.dev/test/module",
				Coordinate: "opmodel.dev/test/module v0.1.0",
			})}

			result, mi, _ := reconcileFailing(ctx, "acquire-terminal-mr", renderer)

			Expect(result.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))
			stalled := apimeta.FindStatusCondition(mi.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.ResolutionFailedReason))

			deleteInstance(ctx, "acquire-terminal-mr")
		})

		It("retries a registry failure during render on the backoff", func() {
			ctx := context.Background()
			// A registry fetch failure after acquisition, as the library
			// returns it from the render build.
			renderer := &stubRenderer{err: fmt.Errorf("rendering module instance: %w", &oerrors.FetchError{
				Kind: oerrors.FetchUnreachable,
				Err:  errors.New(`cannot fetch opmodel.dev/catalogs/opm@v4.6.0: dial tcp registry.example:443: connection refused`),
			})}

			result, mi, es := reconcileFailing(ctx, "render-fetch-transient-mr", renderer)

			Expect(result.RequeueAfter).To(Equal(opmreconcile.ComputeBackoff(1)))
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.ResolutionFailedReason))
			Expect(ready.Message).To(Equal(renderer.err.Error()), "the status message is the render error unchanged")
			Expect(apimeta.FindStatusCondition(mi.Status.Conditions, status.StalledCondition)).To(BeNil())
			Expect(es).To(ContainElement(SatisfyAll(
				HavePrefix(corev1.EventTypeWarning+" "+status.ResolutionFailedReason),
				ContainSubstring(renderer.err.Error()),
			)))

			deleteInstance(ctx, "render-fetch-transient-mr")
		})

		It("stalls an acquisition failure the library does not classify", func() {
			ctx := context.Background()
			renderer := &stubRenderer{err: acquireErr(errors.New(`invalid version "not-a-version"`))}

			result, mi, _ := reconcileFailing(ctx, "acquire-unclassified-mr", renderer)

			Expect(result.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))
			stalled := apimeta.FindStatusCondition(mi.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.ResolutionFailedReason))

			deleteInstance(ctx, "acquire-unclassified-mr")
		})
	})

	Context("Event emission", func() {
		It("should emit Applied and ReconciliationSucceeded events after successful reconcile", func() {
			ctx := context.Background()

			createModuleInstance(ctx, "event-apply-mr")

			recorder := events.NewFakeRecorder(10)
			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "event-apply-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should emit Warning event on apply failure", func() {
			ctx := context.Background()

			createModuleInstance(ctx, "event-applyfail-mr")

			nn := types.NamespacedName{Name: "event-applyfail-mr", Namespace: namespace}

			// First reconcile adds finalizer (use real reconciler).
			realReconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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

			recorder := events.NewFakeRecorder(10)
			failReconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "event-applyfail-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should emit Suspended event when reconcile is suspended", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-suspend-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			recorder := events.NewFakeRecorder(10)
			reconciler := &ModuleInstanceReconciler{
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

			mr := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-resume-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Suspend: true,
					Module:  releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values:  &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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
			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Spec.Suspend = false
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())

			// Fresh recorder for the resume reconcile.
			resumeRecorder := events.NewFakeRecorder(10)
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "event-resume-mr", Namespace: namespace},
			})).To(Succeed())
		})

		It("should emit NoOp event when digests match", func() {
			ctx := context.Background()

			createModuleInstance(ctx, "event-noop-mr")

			reconciler := &ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
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
			noopRecorder := events.NewFakeRecorder(10)
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
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "event-noop-mr", Namespace: namespace},
			})).To(Succeed())
		})

	})
})

// failOnRenderRenderer fails the running spec if the reconciler renders: the
// operator's own instance must be refused before any render.
type failOnRenderRenderer struct{}

func (failOnRenderRenderer) RenderModule(
	_ context.Context,
	name, namespace, _, _ string,
	_ *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	Fail(fmt.Sprintf("render called for ModuleInstance %s/%s", namespace, name))
	return nil, nil
}
