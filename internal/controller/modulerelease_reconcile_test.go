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
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/status"
)

var _ = Describe("ModuleRelease Reconcile Loop", func() {
	const namespace = "default"

	// createReadyOCIRepository creates an OCIRepository with Ready=True and a valid artifact.
	createReadyOCIRepository := func(ctx context.Context, name string) {
		repo := &sourcev1.OCIRepository{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: sourcev1.OCIRepositorySpec{
				URL:      "oci://example.com/" + name,
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}
		Expect(k8sClient.Create(ctx, repo)).To(Succeed())

		// Set status to ready with artifact.
		Eventually(func() error {
			var latest sourcev1.OCIRepository
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: name, Namespace: namespace,
			}, &latest); err != nil {
				return err
			}
			latest.Status.Artifact = &fluxmeta.Artifact{
				URL:            "http://source-controller/" + name + ".tar.gz",
				Revision:       "v1.0.0@sha256:abc123",
				Digest:         "sha256:abc123",
				Path:           "ocirepository/" + namespace + "/" + name + "/sha256:abc123.tar.gz",
				LastUpdateTime: metav1.Now(),
			}
			latest.Status.Conditions = []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "Succeeded",
				},
			}
			return k8sClient.Status().Update(ctx, &latest)
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	}

	createModuleRelease := func(ctx context.Context, name, sourceName string) *releasesv1alpha1.ModuleRelease {
		mr := &releasesv1alpha1.ModuleRelease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: releasesv1alpha1.ModuleReleaseSpec{
				SourceRef: releasesv1alpha1.SourceReference{
					APIVersion: "source.toolkit.fluxcd.io/v1",
					Kind:       "OCIRepository",
					Name:       sourceName,
				},
				Module: releasesv1alpha1.ModuleReference{
					Path: "opmodel.dev/test/module",
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

			createReadyOCIRepository(ctx, "full-reconcile-repo")
			createModuleRelease(ctx, "full-reconcile-mr", "full-reconcile-repo")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				ArtifactFetcher: &copyDirFetcher{sourceDir: testModuleDir()},
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

			// SourceReady=True
			srcReady := apimeta.FindStatusCondition(mr.Status.Conditions, status.SourceReadyCondition)
			Expect(srcReady).NotTo(BeNil())
			Expect(srcReady.Status).To(Equal(metav1.ConditionTrue))

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

			// Source status populated
			Expect(mr.Status.Source).NotTo(BeNil())
			Expect(mr.Status.Source.ArtifactRevision).To(Equal("v1.0.0@sha256:abc123"))

			// ObservedGeneration set
			Expect(mr.Status.ObservedGeneration).To(Equal(mr.Generation))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "full-reconcile-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "full-reconcile-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Suspend check", func() {
		It("should skip reconciliation when suspend is true", func() {
			ctx := context.Background()

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspended-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Suspend: true,
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "any-source",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ArtifactFetcher: &stubFetcher{},
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

			// Verify the Reconciling condition is set with Suspended reason.
			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			reconciling := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReconcilingCondition)
			Expect(reconciling).NotTo(BeNil())
			Expect(reconciling.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconciling.Reason).To(Equal(status.SuspendedReason))

			// Cleanup
			Expect(k8sClient.Delete(ctx, mr)).To(Succeed())
		})
	})

	Context("Source not ready", func() {
		It("should return SoftBlocked when source is not ready", func() {
			ctx := context.Background()

			// Create OCIRepository without ready status.
			repo := &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-ready-repo",
					Namespace: namespace,
				},
				Spec: sourcev1.OCIRepositorySpec{
					URL:      "oci://example.com/not-ready",
					Interval: metav1.Duration{Duration: time.Minute},
				},
			}
			Expect(k8sClient.Create(ctx, repo)).To(Succeed())

			createModuleRelease(ctx, "src-not-ready-mr", "not-ready-repo")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ArtifactFetcher: &stubFetcher{},
			}

			nn := types.NamespacedName{Name: "src-not-ready-mr", Namespace: namespace}

			// First reconcile adds finalizer.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Second reconcile hits source not ready.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			// Verify SourceReady=False condition.
			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &mr)).To(Succeed())

			srcReady := apimeta.FindStatusCondition(mr.Status.Conditions, status.SourceReadyCondition)
			Expect(srcReady).NotTo(BeNil())
			Expect(srcReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(srcReady.Reason).To(Equal(status.SourceNotReadyReason))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "src-not-ready-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, repo)).To(Succeed())
		})
	})

	Context("No-op detection", func() {
		It("should skip apply on second reconcile when digests match", func() {
			ctx := context.Background()

			createReadyOCIRepository(ctx, "noop-repo")
			createModuleRelease(ctx, "noop-mr", "noop-repo")

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				ArtifactFetcher: &copyDirFetcher{sourceDir: testModuleDir()},
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
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "noop-repo", Namespace: namespace},
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
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "any-source",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			reconciler := &ModuleReleaseReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ArtifactFetcher: &stubFetcher{},
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

			createReadyOCIRepository(ctx, "delete-prune-repo")

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-prune-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune: true,
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "delete-prune-repo",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module"},
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
				ArtifactFetcher: &copyDirFetcher{sourceDir: testModuleDir()},
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

			// Cleanup source.
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-prune-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Deletion with prune disabled", func() {
		It("should remove finalizer without deleting resources", func() {
			ctx := context.Background()

			createReadyOCIRepository(ctx, "delete-orphan-repo")

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-orphan-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune: false,
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "delete-orphan-repo",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module"},
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
				ArtifactFetcher: &copyDirFetcher{sourceDir: testModuleDir()},
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
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-orphan-repo", Namespace: namespace},
			})).To(Succeed())
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
					Prune: true,
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "any-source",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test"},
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
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ArtifactFetcher: &stubFetcher{},
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

			createReadyOCIRepository(ctx, "delete-suspend-repo")

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-suspend-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Prune: true,
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "delete-suspend-repo",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module"},
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
				ArtifactFetcher: &copyDirFetcher{sourceDir: testModuleDir()},
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

			// Cleanup source.
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-suspend-repo", Namespace: namespace},
			})).To(Succeed())
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
					Prune: true,
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "any-source",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test"},
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
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ArtifactFetcher: &stubFetcher{},
			}

			// Reconcile should fail — prune cannot delete the non-existent GVK resource.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).To(HaveOccurred())

			// Verify finalizer is still present (not removed due to partial failure).
			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&updated, opmreconcile.FinalizerName)).To(BeTrue())

			// Cleanup: remove finalizer manually so the object can be deleted.
			controllerutil.RemoveFinalizer(&updated, opmreconcile.FinalizerName)
			Expect(k8sClient.Update(ctx, &updated)).To(Succeed())
		})
	})
})
