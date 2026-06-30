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
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// These tests exercise KernelPackageRenderer — the ModulePackage CR render path —
// against the modulepackage fixtures (test/fixtures/modulepackages/{hello,hello-web,
// podinfo,redis}). Each fixture is an author-written #ModuleInstance that imports its
// published test module and embeds it via #module. They are the registry-backed
// siblings of the module-renderer integration test, rendering the same resources via
// an authored instance.cue (ModulePackage) that the ModuleInstance path synthesizes
// from Go.
//
// Embedding a published #Module under #ModuleInstance.#module re-unifies the
// closed module against #Module. This only loads on a core whose #Module
// declares modulePath!/version! as author-supplied identity; the earlier
// self-referential shape failed with "#module.metadata.modulePath: field not
// allowed". This test is therefore the regression guard for that core fix.
//
// Run with: task dev:test:local (skips automatically without the local registry).

var _ = Describe("KernelPackageRenderer Integration", func() {
	Context("when the platform store is empty", func() {
		It("returns ErrPlatformNotReady or ErrUnsupportedKind before compiling", func() {
			// With no materialized platform, the renderer must not reach Compile.
			// It either short-circuits on platform readiness, or — if the fixture
			// imports are unresolvable in this environment — fails the load before
			// the platform gate. Either way it must not panic or render.
			fixtureDir, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "modulepackages", "hello"))
			Expect(err).NotTo(HaveOccurred())

			renderer := &render.KernelPackageRenderer{
				Kernel:      kernel.New(),
				Store:       platformstore.NewStore(),
				RuntimeName: core.LabelManagedByControllerValue,
			}

			_, result, err := renderer.Render(ctx, fixtureDir)
			Expect(result).To(BeNil())
			Expect(err).To(HaveOccurred())
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
			// catalog provides the configmap-transformer the fixture module matches.
			catalogPath := os.Getenv("OPM_TEST_CATALOG_PATH")
			if catalogPath == "" {
				catalogPath = "opmodel.dev/catalogs/opm"
			}
			// Pin the catalog subscription to the version the authored package
			// targets (test/fixtures/modulepackages/hello is core@v1 and pins
			// catalogs/opm@v1 = v1.0.0-alpha). Resource/transformer FQNs are
			// version-qualified, so a subscription that resolves a different
			// catalog version would leave the component unmatched — independent of
			// the render-path fix. The lower bound is prerelease-inclusive because
			// plain ">=1.0.0" excludes -alpha tags.
			catalogRange := os.Getenv("OPM_TEST_CATALOG_RANGE")
			if catalogRange == "" {
				catalogRange = ">=1.0.0-alpha"
			}
			plat, err := k.SynthesizePlatform(ctx, synth.PlatformInput{
				Name: "cluster",
				Type: "kubernetes",
				Subscriptions: map[string]synth.SubscriptionSpec{
					catalogPath: {Filter: &synth.FilterSpec{Range: catalogRange}},
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

		// Every modulepackage fixture is an author-written #ModuleInstance that
		// imports its published test module and embeds it via #module. They all
		// exercise the same load+embed regression guard; the opm catalog pinned
		// above provides the configmap/deployment/service/statefulset transformers
		// each module matches, so they render against the one materialized platform.
		for _, pkg := range []string{"hello", "hello-web", "podinfo", "redis"} {
			It("loads the authored "+pkg+" release fixture (imported #module) and renders its resources", func() {
				fixtureDir, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "modulepackages", pkg))
				Expect(err).NotTo(HaveOccurred())

				renderer := &render.KernelPackageRenderer{
					Kernel:      k,
					Store:       store,
					Registry:    registry,
					RuntimeName: core.LabelManagedByControllerValue,
				}

				kind, res, err := renderer.Render(ctx, fixtureDir)

				// The key regression assertion: loading + embedding the imported
				// #Module must NOT fail with "field not allowed". A self-referential
				// #Module identity shape would surface that error here.
				Expect(err).NotTo(HaveOccurred(),
					"loading the authored release with an imported #Module must not fail "+
						"(a self-referential #Module identity would fail with 'field not allowed')")
				Expect(kind).To(Equal(render.KindModuleInstance))
				Expect(res).NotTo(BeNil())
				Expect(res.Resources).NotTo(BeEmpty(),
					"the %s fixture must compile to at least one resource", pkg)

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
						"module-instance uuid must be non-empty (catalog ownership labels must flow)")
				}

				Expect(res.InventoryEntries).To(HaveLen(len(res.Resources)))
			})
		}
	})
})
