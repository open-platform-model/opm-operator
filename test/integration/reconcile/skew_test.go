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
	"errors"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/kernel"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// skewedCatalogVersion is a published build of the test catalog older than
// the one the fixture modules pin (fixtures.CatalogVersion), so a platform
// pinned to it puts the fixtures in catalog version skew (0019 D7/D18: the
// module requires a NEWER build than the platform carries).
// OPM_TEST_SKEWED_CATALOG_VERSION overrides it for a seeded registry.
func skewedCatalogVersion() string {
	if v := os.Getenv("OPM_TEST_SKEWED_CATALOG_VERSION"); v != "" {
		return v
	}
	return "4.0.0"
}

// Catalog version skew end to end (0019 D7/D18): the fixture module pins a
// newer catalog build than the platform carries. Under Refuse the render is
// refused before evaluation with the typed skew error and the ModuleInstance
// reports SkewRefused; under Warn the render proceeds and the skew is a
// RenderWarning event, emitted once. Registry-backed: skips without a
// mapping (fails under OPM_TEST_REGISTRY_FORCE=1).
var _ = Describe("Catalog version skew (registry-backed)", func() {
	var (
		k        *kernel.Kernel
		registry string
	)

	BeforeEach(func() {
		skipIfNoTestRegistry()
		if skewedCatalogVersion() == testCatalogVersion() {
			registrySkip("no catalog build older than " + testCatalogVersion() + " to skew against")
		}
		registry = os.Getenv("CUE_REGISTRY")
		k = kernel.New(kernel.WithRegistry(registry))
	})

	It("refuses the render under Refuse with the typed skew error naming the path and both versions", func() {
		store := generatedPlatformStoreAt(k, registry, skewedCatalogVersion(), kernel.SkewRefuse)
		renderer := &render.KernelModuleRenderer{
			Kernel:      k,
			Store:       store,
			Registry:    registry,
			RuntimeName: core.LabelManagedByControllerValue,
		}

		hello := fixtures.Must(GinkgoT(), "hello")
		res, err := renderer.RenderModule(ctx, "skew-hello", "default", hello.ModulePath, hello.Tag(), emptyValues())
		Expect(res).To(BeNil())
		Expect(err).To(HaveOccurred())

		var skew *oerrors.SkewError
		Expect(errors.As(err, &skew)).To(BeTrue(), "the refusal must carry *oerrors.SkewError, got %v", err)
		Expect(skew.Path).To(Equal(testCatalogPath()))
		Expect(skew.ModuleVersion).To(Equal("v" + testCatalogVersion()))
		Expect(skew.PlatformVersion).To(Equal("v" + skewedCatalogVersion()))
		Expect(store.Leased()).To(BeEmpty(), "a refused render releases its lease")
	})

	It("stalls a ModuleInstance with reason SkewRefused under Refuse", func() {
		store := generatedPlatformStoreAt(k, registry, skewedCatalogVersion(), kernel.SkewRefuse)
		recorder := events.NewFakeRecorder(16)
		reconciler := &opmcontroller.ModuleInstanceReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			RestConfig:      cfg,
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   recorder,
			Renderer: &render.KernelModuleRenderer{
				Kernel:      k,
				Store:       store,
				Registry:    registry,
				RuntimeName: core.LabelManagedByControllerValue,
			},
		}

		hello := fixtures.Must(GinkgoT(), "hello")
		mi := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "skew-refused-mi", Namespace: "default"},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module: releasesv1alpha1.ModuleReference{Path: hello.ModulePath, Version: hello.Tag()},
				Values: emptyValues(),
			},
		}
		Expect(k8sClient.Create(ctx, mi)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, mi)).To(Succeed())
			// The finalizer path needs one more reconcile to remove it.
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: keyOfObject(mi)})
		})

		req := reconcile.Request{NamespacedName: keyOfObject(mi)}
		_, err := reconciler.Reconcile(ctx, req) // adds the finalizer
		Expect(err).NotTo(HaveOccurred())
		res, err := reconciler.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0), "a skew refusal requeues on the stalled recheck")

		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, keyOfObject(mi), &current)).To(Succeed())
		ready := meta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.SkewRefusedReason))
		Expect(ready.Message).To(SatisfyAll(
			ContainSubstring(testCatalogPath()),
			ContainSubstring("v"+testCatalogVersion()),
			ContainSubstring("v"+skewedCatalogVersion()),
		), "the message names the path, the module's build and the platform's build")
		Expect(meta.IsStatusConditionTrue(current.Status.Conditions, status.StalledCondition)).To(BeTrue())
		Expect(current.Status.Inventory).To(BeNil(), "nothing is applied under a refused render")
	})

	It("renders under Warn and reports the skew as a RenderWarning event once", func() {
		store := generatedPlatformStoreAt(k, registry, skewedCatalogVersion(), kernel.SkewWarn)
		renderer := &render.KernelModuleRenderer{
			Kernel:      k,
			Store:       store,
			Registry:    registry,
			RuntimeName: core.LabelManagedByControllerValue,
		}

		hello := fixtures.Must(GinkgoT(), "hello")
		res, err := renderer.RenderModule(ctx, "skew-warn-hello", "default", hello.ModulePath, hello.Tag(), emptyValues())
		Expect(err).NotTo(HaveOccurred(), "under Warn the render proceeds against the platform's build")
		Expect(res.Resources).NotTo(BeEmpty())
		Expect(res.Warnings).To(ContainElement(SatisfyAll(
			ContainSubstring(testCatalogPath()),
			ContainSubstring("v"+testCatalogVersion()),
			ContainSubstring("v"+skewedCatalogVersion()),
		)), "the skew warning names the path and both versions")

		// Through the reconciler: the warning becomes one RenderWarning event
		// and does not repeat on the next reconcile.
		recorder := events.NewFakeRecorder(32)
		reconciler := &opmcontroller.ModuleInstanceReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			RestConfig:      cfg,
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   recorder,
			Renderer:        renderer,
		}
		mi := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "skew-warn-mi", Namespace: "default"},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module: releasesv1alpha1.ModuleReference{Path: hello.ModulePath, Version: hello.Tag()},
				Values: emptyValues(),
			},
		}
		Expect(k8sClient.Create(ctx, mi)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, mi)).To(Succeed())
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: keyOfObject(mi)})
		})

		req := reconcile.Request{NamespacedName: keyOfObject(mi)}
		_, err = reconciler.Reconcile(ctx, req) // adds the finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, keyOfObject(mi), &current)).To(Succeed())
		ready := meta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
		Expect(countRecordedEvents(recorder, status.RenderWarningReason)).To(Equal(1), "one RenderWarning event for the skew")

		// A second reconcile with the same warnings emits no new event. The
		// spec is unchanged, so the loop re-renders and takes the NoOp path.
		_, err = reconciler.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(countRecordedEvents(recorder, status.RenderWarningReason)).To(BeZero(), "unchanged warnings do not repeat")
	})
})

// emptyValues is the `{}` values document: the fixture's #config is all
// defaults, but the instance's values field must still be set to render.
func emptyValues() *releasesv1alpha1.RawValues {
	v := &releasesv1alpha1.RawValues{}
	v.Raw = []byte(`{}`)
	return v
}
