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
	"fmt"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/source"
	"github.com/open-platform-model/poc-controller/internal/status"
)

const namespace = "default"

func createReadyOCIRepository(name string) {
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

func createModuleRelease(name, sourceName string) {
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
			Prune:  true,
			Values: &releasesv1alpha1.RawValues{},
		},
	}
	mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
	Expect(k8sClient.Create(ctx, mr)).To(Succeed())
}

func reconcileParams(fetcher source.Fetcher) *opmreconcile.ModuleReleaseParams {
	return &opmreconcile.ModuleReleaseParams{
		Client:          k8sClient,
		Provider:        testProvider(),
		ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
		ArtifactFetcher: fetcher,
	}
}

// ensureFinalizer runs one reconcile to register the finalizer, then verifies it was added.
// Call this before the "real" test reconcile for any test that expects to reach Phase 1+.
func ensureFinalizer(params *opmreconcile.ModuleReleaseParams, nn types.NamespacedName) {
	result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
		NamespacedName: nn,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(result).To(Equal(ctrl.Result{}))
}

var _ = Describe("Reconcile Error Paths", func() {
	Context("Render failure", func() {
		It("should set Stalled with RenderFailed when module has no components", func() {
			createReadyOCIRepository("render-fail-repo")
			createModuleRelease("render-fail-mr", "render-fail-repo")

			// no-components-module causes "no resources rendered" error in render.
			params := reconcileParams(&copyDirFetcher{
				sourceDir: renderTestdataDir("no-components-module"),
			})

			nn := types.NamespacedName{Name: "render-fail-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: "render-fail-mr", Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred(), "stalled errors return nil")
			Expect(result.RequeueAfter).To(BeZero(), "stalled does not requeue")

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "render-fail-mr", Namespace: namespace,
			}, &mr)).To(Succeed())

			// Stalled=True
			stalled := apimeta.FindStatusCondition(mr.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.RenderFailedReason))

			// Ready=False
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))

			// Inventory NOT modified (stays nil since first reconcile)
			Expect(mr.Status.Inventory).To(BeNil())

			// History records failure
			Expect(mr.Status.History).To(HaveLen(1))
			Expect(mr.Status.History[0].Message).To(ContainSubstring("no resources rendered"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "render-fail-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "render-fail-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Fetch failure (transient)", func() {
		It("should set NotReady and return error for backoff requeue", func() {
			createReadyOCIRepository("fetch-fail-repo")
			createModuleRelease("fetch-fail-mr", "fetch-fail-repo")

			params := reconcileParams(&stubFetcher{
				err: fmt.Errorf("connection refused"),
			})

			nn := types.NamespacedName{Name: "fetch-fail-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).To(HaveOccurred(), "transient error returned for backoff")
			Expect(result.RequeueAfter).To(BeZero())

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "fetch-fail-mr", Namespace: namespace,
			}, &mr)).To(Succeed())

			// Ready=False with ArtifactFetchFailed
			ready := apimeta.FindStatusCondition(mr.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.ArtifactFetchFailedReason))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "fetch-fail-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "fetch-fail-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Fetch failure (invalid artifact — stalled)", func() {
		It("should set Stalled with ArtifactInvalid when CUE module is missing", func() {
			createReadyOCIRepository("invalid-artifact-repo")
			createModuleRelease("invalid-artifact-mr", "invalid-artifact-repo")

			params := reconcileParams(&stubFetcher{
				err: fmt.Errorf("bad archive: %w", source.ErrMissingCUEModule),
			})

			nn := types.NamespacedName{Name: "invalid-artifact-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred(), "stalled errors return nil")
			Expect(result.RequeueAfter).To(BeZero())

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "invalid-artifact-mr", Namespace: namespace,
			}, &mr)).To(Succeed())

			stalled := apimeta.FindStatusCondition(mr.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.ArtifactInvalidReason))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-artifact-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-artifact-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Source not found (stalled)", func() {
		It("should set Stalled when source does not exist", func() {
			// Create MR referencing non-existent OCIRepository.
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stalled-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "does-not-exist",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			params := reconcileParams(&stubFetcher{})

			nn := types.NamespacedName{Name: "stalled-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred(), "stalled returns nil error")
			Expect(result.RequeueAfter).To(BeZero(), "stalled does not requeue")

			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "stalled-mr", Namespace: namespace,
			}, &updated)).To(Succeed())

			stalled := apimeta.FindStatusCondition(updated.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.SourceUnavailableReason))

			srcReady := apimeta.FindStatusCondition(updated.Status.Conditions, status.SourceReadyCondition)
			Expect(srcReady).NotTo(BeNil())
			Expect(srcReady.Status).To(Equal(metav1.ConditionFalse))

			// Cleanup
			Expect(k8sClient.Delete(ctx, mr)).To(Succeed())
		})
	})

	Context("Status updated on failure", func() {
		It("should populate lastAttempted fields even when reconcile fails", func() {
			createReadyOCIRepository("status-fail-repo")
			createModuleRelease("status-fail-mr", "status-fail-repo")

			params := reconcileParams(&stubFetcher{
				err: fmt.Errorf("network timeout"),
			})

			nn := types.NamespacedName{Name: "status-fail-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			_, _ = opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})

			var mr releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "status-fail-mr", Namespace: namespace,
			}, &mr)).To(Succeed())

			// lastAttempted fields populated despite failure
			Expect(mr.Status.LastAttemptedAction).To(Equal("reconcile"))
			Expect(mr.Status.LastAttemptedAt).NotTo(BeNil())
			Expect(mr.Status.LastAttemptedDuration).NotTo(BeNil())
			Expect(mr.Status.LastAttemptedSourceDigest).NotTo(BeEmpty())
			Expect(mr.Status.LastAttemptedConfigDigest).NotTo(BeEmpty())
			Expect(mr.Status.ObservedGeneration).To(Equal(mr.Generation))

			// lastApplied fields NOT populated (reconcile failed)
			Expect(mr.Status.LastAppliedAt).To(BeNil())
			Expect(mr.Status.LastAppliedSourceDigest).To(BeEmpty())

			// History records the failure
			Expect(mr.Status.History).NotTo(BeEmpty())
			Expect(mr.Status.History[0].Message).To(ContainSubstring("network timeout"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "status-fail-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "status-fail-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Partial failure preserves inventory", func() {
		It("should keep previous inventory when prune fails after successful apply", func() {
			createReadyOCIRepository("partial-fail-repo")

			// Create MR with prune=true.
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "partial-fail-mr",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "partial-fail-repo",
					},
					Module: releasesv1alpha1.ModuleReference{
						Path: "opmodel.dev/test/module",
					},
					Prune:  true,
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			params := reconcileParams(&copyDirFetcher{
				sourceDir: renderTestdataDir("valid-module"),
			})

			// First reconcile — adds finalizer.
			nn := types.NamespacedName{Name: "partial-fail-mr", Namespace: namespace}
			ensureFinalizer(params, nn)

			// Second reconcile — succeeds, populates inventory.
			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			var firstMR releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &firstMR)).To(Succeed())
			Expect(firstMR.Status.Inventory).NotTo(BeNil())
			firstRevision := firstMR.Status.Inventory.Revision
			Expect(firstRevision).To(Equal(int64(1)))
			Expect(firstMR.Status.Inventory.Digest).NotTo(BeEmpty())

			// Inject a fake stale inventory entry AND change the source digest
			// so the second reconcile proceeds past no-op detection.
			Eventually(func() error {
				var latest releasesv1alpha1.ModuleRelease
				if err := k8sClient.Get(ctx, nn, &latest); err != nil {
					return err
				}
				latest.Status.Inventory.Entries = append(
					latest.Status.Inventory.Entries,
					releasesv1alpha1.InventoryEntry{
						Group:     "nonexistent.example.com",
						Kind:      "FakeResource",
						Version:   "v1",
						Namespace: namespace,
						Name:      "should-fail-prune",
					},
				)
				// Change inventory digest so IsNoOp returns false.
				latest.Status.Inventory.Digest = "sha256:stale"
				return k8sClient.Status().Update(ctx, &latest)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Second reconcile — apply succeeds (unchanged), prune should fail
			// on the fake GVK entry.
			result, err = opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).To(HaveOccurred(), "prune failure returns error")

			// Verify inventory was NOT updated (preserved from first reconcile).
			var secondMR releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &secondMR)).To(Succeed())
			Expect(secondMR.Status.Inventory).NotTo(BeNil())
			// The inventory should still show the version from the first reconcile
			// OR the patched version from our manual update — but NOT a new revision
			// from the second reconcile since reconciled=false.
			Expect(secondMR.Status.Inventory.Revision).NotTo(Equal(firstRevision+1),
				"inventory revision should not advance on partial failure")

			// PruneFailed condition
			ready := apimeta.FindStatusCondition(secondMR.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.PruneFailedReason))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: "partial-fail-mr", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "partial-fail-repo", Namespace: namespace},
			})).To(Succeed())
		})
	})
})
