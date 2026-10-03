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
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// renderTestPath is where the ModulePackage specs in this file place
// instance.cue inside the stub artifact.
const renderTestPath = "releases/app"

// newRenderTestNamespace creates a namespace of its own for a spec, so the
// stub ConfigMap ("test-module") never meets one another spec left behind.
func newRenderTestNamespace(ctx context.Context, prefix string) string {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: prefix + "-"}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	return ns.Name
}

// createRenderTestInstance creates an operator-owned ModuleInstance.
func createRenderTestInstance(ctx context.Context, namespace, name string) types.NamespacedName {
	mi := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
		},
	}
	Expect(k8sClient.Create(ctx, mi)).To(Succeed())
	return types.NamespacedName{Name: name, Namespace: namespace}
}

// createRenderTestPackage creates a ready OCIRepository and a ModulePackage
// sourcing renderTestPath from it.
func createRenderTestPackage(ctx context.Context, namespace, name string) types.NamespacedName {
	src := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-src", Namespace: namespace},
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
		Revision:       "main@sha256:aaa",
		Digest:         "sha256:aaa",
		Path:           "ocirepository/" + namespace + "/" + src.Name + "/aaa.tar.gz",
		LastUpdateTime: metav1.Now(),
	}
	Expect(k8sClient.Status().Update(ctx, src)).To(Succeed())

	pkg := &releasesv1alpha1.ModulePackage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModulePackageSpec{
			SourceRef: releasesv1alpha1.SourceReference{Kind: opmsource.SourceKindOCIRepository, Name: src.Name},
			Path:      renderTestPath,
			Interval:  metav1.Duration{Duration: time.Minute},
			Prune:     true,
		},
	}
	Expect(k8sClient.Create(ctx, pkg)).To(Succeed())
	return types.NamespacedName{Name: name, Namespace: namespace}
}

// expectReady asserts that conditions carry Ready=True.
func expectReady(nn types.NamespacedName, conditions []metav1.Condition) {
	ready := apimeta.FindStatusCondition(conditions, status.ReadyCondition)
	Expect(ready).NotTo(BeNil(), "%s has a Ready condition", nn)
	Expect(ready.Status).To(Equal(metav1.ConditionTrue), "%s: reason=%s message=%s", nn, ready.Reason, ready.Message)
}

var _ = Describe("Render memory", func() {
	It("drops the rendered resources once a ModuleInstance converts them", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "drop-mi")
		nn := createRenderTestInstance(ctx, ns, "drop-mi")

		renderer := &stubRenderer{}
		r := &ModuleInstanceReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Renderer:        renderer,
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn}) // adds the finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		last := renderer.lastResult()
		Expect(last).NotTo(BeNil())
		Expect(last.Resources).To(BeNil(), "the CUE-backed resources are dropped after conversion")
		Expect(last.InventoryEntries).To(HaveLen(1), "plain data on the result is untouched")

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		expectReady(nn, mi.Status.Conditions)
		Expect(mi.Status.Inventory).NotTo(BeNil())
		Expect(mi.Status.Inventory.Count).To(Equal(int64(1)))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: ns}, &corev1.ConfigMap{})).To(Succeed())
	})

	It("drops the rendered resources once a ModulePackage converts them", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "drop-mp")
		nn := createRenderTestPackage(ctx, ns, "drop-mp")

		renderer := &stubPackageRenderer{result: stubRenderResult(ns, nil)}
		r := &ModulePackageReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Fetcher:         &stubFetcher{pathInArtifact: renderTestPath},
			Renderer:        renderer,
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn}) // adds the finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		last := renderer.lastResult()
		Expect(last).NotTo(BeNil())
		Expect(last.Resources).To(BeNil(), "the CUE-backed resources are dropped after conversion")
		Expect(last.InventoryEntries).To(HaveLen(1), "plain data on the result is untouched")

		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		expectReady(nn, pkg.Status.Conditions)
		Expect(pkg.Status.Inventory).NotTo(BeNil())
		Expect(pkg.Status.Inventory.Count).To(Equal(int64(1)))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-module", Namespace: ns}, &corev1.ConfigMap{})).To(Succeed())
	})
})
