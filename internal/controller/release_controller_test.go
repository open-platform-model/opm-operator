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
	"os"
	"path/filepath"
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
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// stubFetcher creates a placeholder release.cue at dir/<path>/release.cue so
// the reconciler's path navigation step succeeds without an HTTP download.
type stubFetcher struct {
	pathInArtifact string
	skipWriteFile  bool
	err            error
}

func (s *stubFetcher) Fetch(_ context.Context, _ string, _ string, dir string, _ opmsource.FetchOptions) error {
	if s.err != nil {
		return s.err
	}
	if s.skipWriteFile {
		return nil
	}
	target := filepath.Join(dir, s.pathInArtifact)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(target, "release.cue"), []byte("package release\n"), 0o644)
}

// stubReleaseRenderer returns a pre-built RenderResult (or error) without
// evaluating CUE.
type stubReleaseRenderer struct {
	kind   string
	result *render.RenderResult
	err    error
}

func (s *stubReleaseRenderer) Render(_ context.Context, _ string, _ *provider.Provider) (string, *render.RenderResult, error) {
	if s.err != nil {
		return s.kind, nil, s.err
	}
	if s.kind == "" {
		return render.KindModuleRelease, s.result, nil
	}
	return s.kind, s.result, nil
}

var _ = Describe("Release Controller", func() {
	const namespace = "default"

	newOCIRepo := func(name, ns string) *sourcev1.OCIRepository {
		return &sourcev1.OCIRepository{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: sourcev1.OCIRepositorySpec{
				URL:      "oci://example.com/repo",
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}
	}

	markOCIReady := func(obj *sourcev1.OCIRepository, rev, digest string) {
		obj.Status.Conditions = []metav1.Condition{{
			Type:               fluxmeta.ReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             "Succeeded",
			Message:            "ready",
			LastTransitionTime: metav1.Now(),
		}}
		obj.Status.Artifact = &fluxmeta.Artifact{
			URL:            "http://source-controller/artifact.tar.gz",
			Revision:       rev,
			Digest:         digest,
			Path:           "ocirepository/default/" + obj.Name + "/" + digest + ".tar.gz",
			LastUpdateTime: metav1.Now(),
		}
	}

	createRelease := func(ctx context.Context, name, path string, suspend bool, dependsOn []fluxmeta.NamespacedObjectReference) *releasesv1alpha1.Release {
		rel := &releasesv1alpha1.Release{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: releasesv1alpha1.ReleaseSpec{
				SourceRef: releasesv1alpha1.SourceReference{
					Kind: opmsource.SourceKindOCIRepository,
					Name: name + "-src",
				},
				Path:      path,
				Interval:  metav1.Duration{Duration: time.Minute},
				Prune:     true,
				Suspend:   suspend,
				DependsOn: dependsOn,
			},
		}
		Expect(k8sClient.Create(ctx, rel)).To(Succeed())
		return rel
	}

	buildReconciler := func(fetcher opmsource.Fetcher, renderer render.ReleaseRenderer) *ReleaseReconciler {
		return &ReleaseReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			Provider:        testProvider(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(10),
			Fetcher:         fetcher,
			Renderer:        renderer,
		}
	}

	reconcileTwice := func(ctx context.Context, r *ReleaseReconciler, nn types.NamespacedName) reconcile.Result {
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		return result
	}

	Context("Happy path", func() {
		It("applies resources and populates status", func() {
			ctx := context.Background()
			name := "happy-release"

			src := newOCIRepo(name+"-src", namespace)
			Expect(k8sClient.Create(ctx, src)).To(Succeed())
			markOCIReady(src, "main@sha256:aaa", "sha256:aaa")
			Expect(k8sClient.Status().Update(ctx, src)).To(Succeed())

			createRelease(ctx, name, "releases/app", false, nil)

			fetcher := &stubFetcher{pathInArtifact: "releases/app"}
			renderer := &stubReleaseRenderer{result: stubRenderResult(namespace, nil)}

			r := buildReconciler(fetcher, renderer)
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			result := reconcileTwice(ctx, r, nn)
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm)).To(Succeed())

			var got releasesv1alpha1.Release
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			ready := apimeta.FindStatusCondition(got.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(got.Status.Source).NotTo(BeNil())
			Expect(got.Status.Source.ArtifactDigest).To(Equal("sha256:aaa"))
			Expect(got.Status.Inventory).NotTo(BeNil())
			Expect(got.Status.Inventory.Count).To(Equal(int64(1)))
			Expect(got.Status.LastAppliedRenderDigest).NotTo(BeEmpty())
			Expect(got.Status.History).NotTo(BeEmpty())

			Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &got)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})
	})

	Context("Source not ready", func() {
		It("sets Ready=False with SourceNotReady and requeues", func() {
			ctx := context.Background()
			name := "src-not-ready"

			src := newOCIRepo(name+"-src", namespace)
			Expect(k8sClient.Create(ctx, src)).To(Succeed()) // no Ready condition

			createRelease(ctx, name, "releases/app", false, nil)

			r := buildReconciler(&stubFetcher{}, &stubReleaseRenderer{})
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			result := reconcileTwice(ctx, r, nn)
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

			var got releasesv1alpha1.Release
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			ready := apimeta.FindStatusCondition(got.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.SourceNotReadyReason))

			Expect(k8sClient.Delete(ctx, &got)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})
	})

	Context("Path not found", func() {
		It("sets Stalled=True with PathNotFound when release.cue missing", func() {
			ctx := context.Background()
			name := "path-missing"

			src := newOCIRepo(name+"-src", namespace)
			Expect(k8sClient.Create(ctx, src)).To(Succeed())
			markOCIReady(src, "main@sha256:bbb", "sha256:bbb")
			Expect(k8sClient.Status().Update(ctx, src)).To(Succeed())

			createRelease(ctx, name, "does/not/exist", false, nil)

			fetcher := &stubFetcher{skipWriteFile: true}
			r := buildReconciler(fetcher, &stubReleaseRenderer{})
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			_ = reconcileTwice(ctx, r, nn)

			var got releasesv1alpha1.Release
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			stalled := apimeta.FindStatusCondition(got.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Reason).To(Equal(status.PathNotFoundReason))

			Expect(k8sClient.Delete(ctx, &got)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})
	})

	Context("Suspend", func() {
		It("sets Suspended reason without reconciling", func() {
			ctx := context.Background()
			name := "suspended-release"

			src := newOCIRepo(name+"-src", namespace)
			Expect(k8sClient.Create(ctx, src)).To(Succeed())

			createRelease(ctx, name, "releases/app", true, nil)

			r := buildReconciler(&stubFetcher{}, &stubReleaseRenderer{err: fmt.Errorf("should not render")})
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			_ = reconcileTwice(ctx, r, nn)

			var got releasesv1alpha1.Release
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			ready := apimeta.FindStatusCondition(got.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(status.SuspendedReason))

			Expect(k8sClient.Delete(ctx, &got)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})

		It("resumes reconciliation when suspend flips to false", func() {
			ctx := context.Background()
			name := "resume-release"

			src := newOCIRepo(name+"-src", namespace)
			Expect(k8sClient.Create(ctx, src)).To(Succeed())
			markOCIReady(src, "main@sha256:ccc", "sha256:ccc")
			Expect(k8sClient.Status().Update(ctx, src)).To(Succeed())

			rel := createRelease(ctx, name, "releases/app", true, nil)

			fetcher := &stubFetcher{pathInArtifact: "releases/app"}
			renderer := &stubReleaseRenderer{result: stubRenderResult(namespace, nil)}
			r := buildReconciler(fetcher, renderer)
			nn := types.NamespacedName{Name: name, Namespace: namespace}

			_ = reconcileTwice(ctx, r, nn)

			// Un-suspend
			Expect(k8sClient.Get(ctx, nn, rel)).To(Succeed())
			rel.Spec.Suspend = false
			Expect(k8sClient.Update(ctx, rel)).To(Succeed())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var got releasesv1alpha1.Release
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			ready := apimeta.FindStatusCondition(got.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			var cm corev1.ConfigMap
			if k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: namespace}, &cm) == nil {
				Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
			}
			Expect(k8sClient.Delete(ctx, &got)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})
	})

	Context("Unsupported kind", func() {
		It("sets Stalled=True with UnsupportedKind", func() {
			ctx := context.Background()
			name := "unsupported-kind"

			src := newOCIRepo(name+"-src", namespace)
			Expect(k8sClient.Create(ctx, src)).To(Succeed())
			markOCIReady(src, "main@sha256:ddd", "sha256:ddd")
			Expect(k8sClient.Status().Update(ctx, src)).To(Succeed())

			createRelease(ctx, name, "releases/app", false, nil)

			fetcher := &stubFetcher{pathInArtifact: "releases/app"}
			renderer := &stubReleaseRenderer{err: fmt.Errorf("%w: BundleRelease rendering is not yet implemented", render.ErrUnsupportedKind)}
			r := buildReconciler(fetcher, renderer)
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			_ = reconcileTwice(ctx, r, nn)

			var got releasesv1alpha1.Release
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			stalled := apimeta.FindStatusCondition(got.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Reason).To(Equal(status.UnsupportedKindReason))

			Expect(k8sClient.Delete(ctx, &got)).To(Succeed())
			Expect(k8sClient.Delete(ctx, src)).To(Succeed())
		})
	})
})
