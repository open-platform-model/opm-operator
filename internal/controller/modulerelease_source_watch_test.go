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
	"sync/atomic"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

var _ = Describe("ModuleRelease Source Watch", func() {
	const namespace = "default"

	Context("ociRepositoryToRequests map function", func() {
		var reconciler *ModuleReleaseReconciler

		BeforeEach(func() {
			reconciler = &ModuleReleaseReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
		})

		It("should enqueue ModuleReleases that reference the changed OCIRepository", func() {
			ctx := context.Background()

			repo := &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "map-test-repo",
					Namespace: namespace,
				},
				Spec: sourcev1.OCIRepositorySpec{
					URL:      "oci://example.com/test",
					Interval: metav1.Duration{Duration: time.Minute},
				},
			}
			Expect(k8sClient.Create(ctx, repo)).To(Succeed())

			mr1 := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mr-refs-repo",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "map-test-repo",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test"},
				},
			}
			Expect(k8sClient.Create(ctx, mr1)).To(Succeed())

			mr2 := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mr-refs-other",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "some-other-repo",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/other"},
				},
			}
			Expect(k8sClient.Create(ctx, mr2)).To(Succeed())

			requests := reconciler.ociRepositoryToRequests(ctx, repo)

			Expect(requests).To(HaveLen(1))
			Expect(requests[0].NamespacedName).To(Equal(types.NamespacedName{
				Name:      "mr-refs-repo",
				Namespace: namespace,
			}))

			// Cleanup
			Expect(k8sClient.Delete(ctx, mr1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, mr2)).To(Succeed())
			Expect(k8sClient.Delete(ctx, repo)).To(Succeed())
		})

		It("should not enqueue ModuleReleases that do not reference the OCIRepository", func() {
			ctx := context.Background()

			repo := &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unreferenced-repo",
					Namespace: namespace,
				},
				Spec: sourcev1.OCIRepositorySpec{
					URL:      "oci://example.com/unreferenced",
					Interval: metav1.Duration{Duration: time.Minute},
				},
			}
			Expect(k8sClient.Create(ctx, repo)).To(Succeed())

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mr-unrelated",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "different-repo",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			requests := reconciler.ociRepositoryToRequests(ctx, repo)
			Expect(requests).To(BeEmpty())

			Expect(k8sClient.Delete(ctx, mr)).To(Succeed())
			Expect(k8sClient.Delete(ctx, repo)).To(Succeed())
		})
	})

	Context("OCIRepository watch triggers reconciliation", func() {
		It("should reconcile ModuleRelease when referenced OCIRepository status changes", func() {
			mgrCtx, mgrCancel := context.WithCancel(context.Background())
			defer mgrCancel()

			var reconcileCount atomic.Int64
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			baseReconciler := &ModuleReleaseReconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				ArtifactFetcher: &stubFetcher{},
				EventRecorder:   record.NewFakeRecorder(10),
			}

			err = ctrl.NewControllerManagedBy(mgr).
				For(&releasesv1alpha1.ModuleRelease{}).
				Watches(&sourcev1.OCIRepository{},
					handler.EnqueueRequestsFromMapFunc(baseReconciler.ociRepositoryToRequests)).
				Named("modulerelease-watch-test").
				Complete(&reconcileCounter{
					ModuleReleaseReconciler: *baseReconciler,
					count:                   &reconcileCount,
				})
			Expect(err).NotTo(HaveOccurred())

			go func() {
				defer GinkgoRecover()
				Expect(mgr.Start(mgrCtx)).To(Succeed())
			}()

			ctx := context.Background()

			// Create the OCIRepository
			repo := &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "watch-test-repo",
					Namespace: namespace,
				},
				Spec: sourcev1.OCIRepositorySpec{
					URL:      "oci://example.com/watch-test",
					Interval: metav1.Duration{Duration: time.Minute},
				},
			}
			Expect(k8sClient.Create(ctx, repo)).To(Succeed())

			// Create a ModuleRelease referencing it
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mr-watch-test",
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					SourceRef: releasesv1alpha1.SourceReference{
						APIVersion: "source.toolkit.fluxcd.io/v1",
						Kind:       "OCIRepository",
						Name:       "watch-test-repo",
					},
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/watch"},
				},
			}
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			// Wait for initial reconciliation from ModuleRelease creation
			Eventually(func() int64 {
				return reconcileCount.Load()
			}, 5*time.Second, 100*time.Millisecond).Should(BeNumerically(">=", 1))

			// Record the count before the OCIRepository update
			countBefore := reconcileCount.Load()

			// Update the OCIRepository status to simulate a new artifact
			Eventually(func() error {
				var latest sourcev1.OCIRepository
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "watch-test-repo", Namespace: namespace,
				}, &latest); err != nil {
					return err
				}
				latest.Status.Artifact = &fluxmeta.Artifact{
					URL:            "http://source-controller/watch-test.tar.gz",
					Revision:       "v1.0.0@sha256:deadbeef",
					Digest:         "sha256:deadbeef",
					Path:           "ocirepository/default/watch-test-repo/sha256:deadbeef.tar.gz",
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

			// The OCIRepository status update should trigger reconciliation of the ModuleRelease
			Eventually(func() int64 {
				return reconcileCount.Load()
			}, 5*time.Second, 100*time.Millisecond).Should(BeNumerically(">", countBefore))

			// Cleanup
			Expect(k8sClient.Delete(ctx, mr)).To(Succeed())
			Expect(k8sClient.Delete(ctx, repo)).To(Succeed())
			mgrCancel()
		})
	})
})

// reconcileCounter wraps ModuleReleaseReconciler to count reconcile calls.
type reconcileCounter struct {
	ModuleReleaseReconciler
	count *atomic.Int64
}

func (r *reconcileCounter) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.count.Add(1)
	return r.ModuleReleaseReconciler.Reconcile(ctx, req)
}
