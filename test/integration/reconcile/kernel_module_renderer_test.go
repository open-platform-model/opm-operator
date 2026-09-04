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

	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/core"
	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// These tests exercise KernelModuleRenderer directly (the ModuleInstance
// reconciler wires it in production). The happy path requires the fixture
// module and its catalog to resolve from CUE_REGISTRY (GHCR under
// `task dev:test`); it is skipped automatically when either is unavailable.

var _ = Describe("KernelModuleRenderer Integration", func() {
	Context("when the platform store is empty", func() {
		It("returns ErrPlatformNotReady without acquiring or rendering", func() {
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
				"testing.opmodel.dev/modules/operator/does-not-exist@v0", "v9.9.9",
				nil)

			Expect(res).To(BeNil())
			Expect(err).To(MatchError(render.ErrPlatformNotReady))
		})
	})

	Context("when the store holds a generated platform", func() {
		var (
			k        *kernel.Kernel
			registry string
			store    *platformstore.Store
		)

		BeforeEach(func() {
			skipIfNoTestRegistry()
			registry = os.Getenv("CUE_REGISTRY")
			k = kernel.New(kernel.WithRegistry(registry))
			// Generate and build the platform module the way the
			// PlatformReconciler does, subscribed to the exact catalog build the
			// fixture module targets: transformer FQNs embed the catalog
			// version, so another build leaves the components unmatched.
			store = generatedPlatformStore(k, registry, kernel.SkewWarn)
		})

		It("renders the fixture module's resources with provenance and inventory, releasing its lease", func() {
			renderer := &render.KernelModuleRenderer{
				Kernel:      k,
				Store:       store,
				Registry:    registry,
				RuntimeName: core.LabelManagedByControllerValue,
			}

			values := &releasesv1alpha1.RawValues{}
			values.Raw = []byte(`{"message": "kernel hello"}`)
			hello := fixtures.Must(GinkgoT(), "hello")
			res, err := renderer.RenderModule(ctx,
				"kernel-hello", "default",
				hello.ModulePath, hello.Tag(),
				values)

			Expect(err).NotTo(HaveOccurred())
			Expect(res).NotTo(BeNil())
			Expect(res.Resources).NotTo(BeEmpty(),
				"the fixture module must render to at least one resource")
			Expect(res.Warnings).To(BeEmpty(), "a module pinning the platform's catalog build renders without warnings")
			Expect(res.ResolvedVersions).NotTo(BeEmpty(), "the build reports the resolved-versions rows (0019 D18)")
			Expect(store.Leased()).To(BeEmpty(), "the render releases its lease on return")

			// Every rendered resource carries instance/component/transformer
			// provenance copied from the kernel's Compiled output, plus the
			// runtime-identity labels that lock the Go/CUE contract between
			// core.LabelManagedByControllerValue and the catalog's #runtimeName.
			for _, r := range res.Resources {
				Expect(r.Instance).NotTo(BeEmpty(), "resource %s missing instance provenance", r)
				Expect(r.Component).NotTo(BeEmpty(), "resource %s missing component provenance", r)
				Expect(r.Transformer).NotTo(BeEmpty(), "resource %s missing transformer provenance", r)

				u, err := r.ToUnstructured()
				Expect(err).NotTo(HaveOccurred())
				labels := u.GetLabels()
				Expect(labels).NotTo(BeNil(), "rendered resource %s must carry labels", u.GetName())
				Expect(labels[core.LabelManagedBy]).To(Equal(core.LabelManagedByControllerValue),
					"managed-by must be opm-controller (Go/CUE contract)")
				Expect(labels[core.LabelModuleInstanceUUID]).NotTo(BeEmpty(),
					"module-instance uuid must be non-empty (catalog ownership labels must continue to flow)")
			}

			// One inventory entry per rendered resource, built via the existing
			// ToUnstructured bridge.
			Expect(res.InventoryEntries).To(HaveLen(len(res.Resources)))
		})
	})
})
