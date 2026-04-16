/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile_test

import (
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	"github.com/open-platform-model/poc-controller/internal/catalog"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/render"
	"github.com/open-platform-model/poc-controller/internal/status"
	"github.com/open-platform-model/poc-controller/internal/synthesis"
	"github.com/open-platform-model/poc-controller/pkg/loader"
)

// End-to-end integration against a real local OCI registry. Exercises the
// full pipeline: CUE synthesis → OCI module resolution → rendering with the
// real catalog provider → SSA apply into envtest.
//
// Bring-up:
//
//	make start-registry
//	make publish-test-module
//	CUE_REGISTRY=<testing+opmodel both → localhost:5000+insecure, then registry.cue.works> \
//	    go test ./test/integration/reconcile/...
//
// See reference_local_registry_setup memory or Makefile:211 for the exact value.
var _ = Describe("End-to-end module resolution", Ordered, func() {
	BeforeEach(func() {
		skipIfNoTestRegistry()
	})

	It("resolves the fixture module from the local registry via CUE", func() {
		// Synthesize a release package that imports the fixture module.
		// CUE loads dependencies using CUE_REGISTRY; the fixture module is
		// resolved from the local registry (testing.opmodel.dev), while its
		// catalog imports (opmodel.dev/core/..., opmodel.dev/opm/...) resolve
		// from the local registry via the opmodel.dev= mapping.
		dir, err := synthesis.SynthesizeRelease(synthesis.ReleaseParams{
			Name:          "e2e-hello",
			Namespace:     "default",
			ModulePath:    "testing.opmodel.dev/test/hello@v0",
			ModuleVersion: "v0.0.1",
		})
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = os.RemoveAll(dir) }()

		cueCtx := cuecontext.New()
		val, err := loader.LoadModulePackage(cueCtx, dir)
		Expect(err).NotTo(HaveOccurred(),
			"CUE must resolve fixture module + catalog deps from the OCI registry")
		Expect(val.Err()).NotTo(HaveOccurred())
	})

	It("surfaces registry errors via RegistryRenderer for an unknown module", func() {
		renderer := &render.RegistryRenderer{}
		_, err := renderer.RenderModule(ctx,
			"e2e-missing",
			"default",
			"testing.opmodel.dev/test/does-not-exist@v0",
			"v0.0.1",
			nil,
			testProvider(),
		)
		Expect(err).To(HaveOccurred(),
			"missing module must surface a resolution error from CUE")
	})

	It("reconciles the fixture end-to-end with the real catalog provider", func() {
		// Load the real kubernetes provider from the workspace catalog
		// composition at catalog/. This references prod composition directly
		// (no copy), so the test tracks production wiring automatically.
		realProv, err := catalog.LoadProvider("../../../catalog", "kubernetes")
		if err != nil {
			Skip(fmt.Sprintf("catalog.LoadProvider failed — "+
				"ensure opmodel.dev catalog modules are published to the local registry: %v", err))
		}

		mrName := "e2e-hello-mr"
		mr := &releasesv1alpha1.ModuleRelease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mrName,
				Namespace: namespace,
			},
			Spec: releasesv1alpha1.ModuleReleaseSpec{
				Module: releasesv1alpha1.ModuleReference{
					Path:    "testing.opmodel.dev/test/hello@v0",
					Version: "v0.0.1",
				},
				Prune:  true,
				Values: &releasesv1alpha1.RawValues{},
			},
		}
		mr.Spec.Values.Raw = []byte(`{"message": "e2e hello"}`)
		Expect(k8sClient.Create(ctx, mr)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: namespace},
			})
		})

		params := &opmreconcile.ModuleReleaseParams{
			Client:          k8sClient,
			Provider:        realProv,
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   record.NewFakeRecorder(10),
			Renderer:        &render.RegistryRenderer{},
		}
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		// Finalizer reconcile.
		ensureFinalizer(params, nn)

		// Full reconcile — exercises the real synthesis → resolution → render →
		// SSA apply pipeline end-to-end.
		result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		// Verify Ready=True and inventory populated.
		var updated releasesv1alpha1.ModuleRelease
		Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
		ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(updated.Status.Inventory).NotTo(BeNil())
		Expect(updated.Status.Inventory.Count).To(BeNumerically(">", 0))

		// Verify the rendered ConfigMap was applied. The hello fixture produces
		// a single ConfigMap carrying values.message.
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: updated.Status.Inventory.Entries[0].Name, Namespace: namespace,
		}, cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("e2e hello"))

		// Delete the ConfigMap so it does not leak to sibling specs.
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, cm)
		})
	})
})
