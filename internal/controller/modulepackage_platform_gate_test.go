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
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

var _ = Describe("ModulePackage platform-gated rendering", func() {
	const namespace = "default"

	readySource := func(ctx context.Context, name, rev, digest string) *sourcev1.OCIRepository {
		src := &sourcev1.OCIRepository{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: sourcev1.OCIRepositorySpec{
				URL:      "oci://example.com/repo",
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}
		Expect(k8sClient.Create(ctx, src)).To(Succeed())
		src.Status.Conditions = []metav1.Condition{{
			Type:               fluxmeta.ReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             "Succeeded",
			Message:            "ready",
			LastTransitionTime: metav1.Now(),
		}}
		src.Status.Artifact = &fluxmeta.Artifact{
			URL:            "http://source-controller/artifact.tar.gz",
			Revision:       rev,
			Digest:         digest,
			Path:           "ocirepository/default/" + name + "/" + digest + ".tar.gz",
			LastUpdateTime: metav1.Now(),
		}
		Expect(k8sClient.Status().Update(ctx, src)).To(Succeed())
		return src
	}

	newModulePackage := func(ctx context.Context, name string) types.NamespacedName {
		rel := &releasesv1alpha1.ModulePackage{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: releasesv1alpha1.ModulePackageSpec{
				SourceRef: releasesv1alpha1.SourceReference{
					Kind: opmsource.SourceKindOCIRepository,
					Name: name + "-src",
				},
				Path:     "releases/app",
				Interval: metav1.Duration{Duration: time.Minute},
				Prune:    true,
			},
		}
		Expect(k8sClient.Create(ctx, rel)).To(Succeed())
		return types.NamespacedName{Name: name, Namespace: namespace}
	}

	Context("when a platform is materialized (5.1)", func() {
		It("renders the release and applies the resulting resources", func() {
			ctx := context.Background()

			// The render+apply success path runs through the PackageRenderer seam
			// the cut-over swaps to KernelPackageRenderer. Exercising the kernel's
			// load→compile internals against a materialized platform requires a live
			// OCI registry, so that is covered by the renderer's own tests; here we
			// assert the reconciler applies and records status when rendering
			// succeeds (the platform-present case), mirroring the ModuleInstance gate
			// test.
			src := readySource(ctx, "gate-apply-rel-src", "main@sha256:a1", "sha256:a1")
			nn := newModulePackage(ctx, "gate-apply-rel")

			reconciler := &ModulePackageReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Fetcher:         &stubFetcher{pathInArtifact: "releases/app"},
				Renderer:        &stubPackageRenderer{result: stubRenderResult(namespace, nil)},
			}

			// First reconcile adds the finalizer; second runs the full pipeline.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())

			var rel releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &rel)).To(Succeed())
			ready := apimeta.FindStatusCondition(rel.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(rel.Status.Inventory).NotTo(BeNil())
			Expect(rel.Status.Inventory.Count).To(Equal(int64(1)))

			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &rel)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})
	})

	Context("when no platform is materialized (5.2)", func() {
		It("blocks with PlatformNotReady, applying and pruning nothing", func() {
			ctx := context.Background()

			// The renderer returns ErrPlatformNotReady (its empty-store behavior is
			// covered registry-free in internal/render). Here we assert the
			// reconciler's gate branch: Ready=False/PlatformNotReady, no apply or
			// prune, a warning event, and an interval requeue — not a stall.
			src := readySource(ctx, "gate-block-rel-src", "main@sha256:b1", "sha256:b1")
			nn := newModulePackage(ctx, "gate-block-rel")

			recorder := events.NewFakeRecorder(10)
			reconciler := &ModulePackageReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   recorder,
				Fetcher:         &stubFetcher{pathInArtifact: "releases/app"},
				Renderer:        &stubPackageRenderer{err: render.ErrPlatformNotReady},
			}

			// First reconcile adds the finalizer; second hits the gate.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "blocked-on-platform returns nil error with requeue")
			// Blocked is a transient requeue on the reconcile interval, not the
			// stalled-recheck interval.
			Expect(result.RequeueAfter).To(Equal(time.Minute))

			var rel releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &rel)).To(Succeed())

			// Ready=False/PlatformNotReady — distinct from a render failure and not
			// a terminal stall.
			ready := apimeta.FindStatusCondition(rel.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.PlatformNotReadyReason))
			Expect(ready.Reason).NotTo(Equal(status.RenderFailedReason))

			stalled := apimeta.FindStatusCondition(rel.Status.Conditions, status.StalledCondition)
			Expect(stalled).To(BeNil())

			// Nothing applied: no inventory recorded and no ConfigMap created.
			Expect(rel.Status.Inventory).To(BeNil())
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &corev1.ConfigMap{})
			Expect(err).To(HaveOccurred())

			// A warning event was emitted for the blocked state.
			var event string
			Eventually(recorder.Events).Should(Receive(&event))
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring(status.PlatformNotReadyReason))

			Expect(k8sClient.Delete(ctx, &rel)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})
	})

	Context("when the platform changes (5.3)", func() {
		It("re-enqueues every ModulePackage in the cluster", func() {
			ctx := context.Background()

			nn1 := newModulePackage(ctx, "gate-enqueue-rel-1")
			nn2 := newModulePackage(ctx, "gate-enqueue-rel-2")

			reconciler := &ModulePackageReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
			}

			platform := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			reqs := reconciler.mapPlatformToModulePackages(ctx, platform)

			Expect(reqs).To(ContainElement(reconcile.Request{NamespacedName: nn1}))
			Expect(reqs).To(ContainElement(reconcile.Request{NamespacedName: nn2}))

			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModulePackage{
				ObjectMeta: metav1.ObjectMeta{Name: nn1.Name, Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModulePackage{
				ObjectMeta: metav1.ObjectMeta{Name: nn2.Name, Namespace: namespace},
			})).To(Succeed())
		})
	})
})
