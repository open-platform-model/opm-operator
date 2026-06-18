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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// These tests exercise KernelModuleRenderer directly (the ModuleRelease
// reconciler now wires it in production). The happy path requires the fixture
// module published to a local OCI registry plus a resolvable catalog to
// materialize the platform against; it is skipped automatically when either is
// unavailable. Run with: task dev:test:local

var _ = Describe("KernelModuleRenderer Integration", func() {
	Context("when the platform store is empty", func() {
		It("returns ErrPlatformNotReady without acquiring or compiling", func() {
			// No registry is configured and the module path is unresolvable: if
			// the gate did not short-circuit, acquisition would fail loudly with
			// a different error. MatchError(ErrPlatformNotReady) proves the
			// renderer returns before any registry I/O.
			renderer := &render.KernelModuleRenderer{
				Kernel:      kernel.New(),
				Store:       platformstore.NewStore(),
				Registry:    "opmodel.dev=localhost:5000+insecure",
				RuntimeName: core.LabelManagedByControllerValue,
			}

			res, err := renderer.RenderModule(ctx,
				"hello", "default",
				"opmodel.dev/modules/test/does-not-exist@v0", "v9.9.9",
				nil)

			Expect(res).To(BeNil())
			Expect(err).To(MatchError(render.ErrPlatformNotReady))
		})
	})

	Context("when the store holds a materialized platform", func() {
		var (
			k        *kernel.Kernel
			registry string
			store    *platformstore.Store
		)

		BeforeEach(func() {
			skipIfNoTestRegistry()
			registry = os.Getenv("CUE_REGISTRY")
			k = kernel.New(kernel.WithRegistry(registry))

			// Materialize a platform via the real synth → materialize path so the
			// store holds the same shape the PlatformReconciler produces. The opm
			// catalog provides the transformers the fixture module matches.
			catalogPath := os.Getenv("OPM_TEST_CATALOG_PATH")
			if catalogPath == "" {
				catalogPath = "opmodel.dev/catalogs/opm"
			}
			plat, err := k.SynthesizePlatform(ctx, synth.PlatformInput{
				Name: "cluster",
				Type: "kubernetes",
				Subscriptions: map[string]synth.SubscriptionSpec{
					catalogPath: {},
				},
			})
			if err != nil {
				Skip("synthesizing platform failed (registry/schema unreachable): " + err.Error())
			}
			mp, err := k.Materialize(ctx, plat)
			if err != nil {
				Skip("materializing platform failed (catalog unreachable): " + err.Error())
			}
			store = platformstore.NewStore()
			store.Set(1, mp)
		})

		It("renders the fixture module's resources with provenance and inventory", func() {
			renderer := &render.KernelModuleRenderer{
				Kernel:      k,
				Store:       store,
				Registry:    registry,
				RuntimeName: core.LabelManagedByControllerValue,
			}

			values := &releasesv1alpha1.RawValues{}
			values.Raw = []byte(`{"message": "kernel hello"}`)
			res, err := renderer.RenderModule(ctx,
				"kernel-hello", "default",
				"opmodel.dev/modules/test/hello@v0", "v0.0.2",
				values)

			Expect(err).NotTo(HaveOccurred())
			Expect(res).NotTo(BeNil())
			Expect(res.Resources).NotTo(BeEmpty(),
				"the fixture module must compile to at least one resource")

			// Every rendered resource carries release/component/transformer
			// provenance copied from the kernel's Compiled output, plus the
			// runtime-identity labels that lock the Go/CUE contract between
			// core.LabelManagedByControllerValue and the catalog's #runtimeName.
			for _, r := range res.Resources {
				Expect(r.Release).NotTo(BeEmpty(), "resource %s missing release provenance", r)
				Expect(r.Component).NotTo(BeEmpty(), "resource %s missing component provenance", r)
				Expect(r.Transformer).NotTo(BeEmpty(), "resource %s missing transformer provenance", r)

				u, err := r.ToUnstructured()
				Expect(err).NotTo(HaveOccurred())
				labels := u.GetLabels()
				Expect(labels).NotTo(BeNil(), "rendered resource %s must carry labels", u.GetName())
				Expect(labels[core.LabelManagedBy]).To(Equal(core.LabelManagedByControllerValue),
					"managed-by must be opm-controller (Go/CUE contract)")
				Expect(labels[core.LabelModuleReleaseUUID]).NotTo(BeEmpty(),
					"module-release uuid must be non-empty (catalog ownership labels must continue to flow)")
			}

			// One inventory entry per rendered resource, built via the existing
			// ToUnstructured bridge.
			Expect(res.InventoryEntries).To(HaveLen(len(res.Resources)))
		})
	})
})
