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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// These tests exercise KernelModuleRenderer directly (the ModuleInstance
// reconciler wires it in production). The happy path requires the fixture
// module and its catalog to resolve from CUE_REGISTRY (GHCR under
// `task dev:test`); it is skipped automatically when either is unavailable.

// valuesFrame is the frame a values error carries for the instance name:
// instance synthesis, the one check of the values against #config.
func valuesFrame(instance string) string {
	return `synthesizing release: Kernel.SynthesizeInstance: instance "` + instance + `": `
}

// configPath is the path a values finding names #config by: synthesis
// reports it inside the instance's #module.
const configPath = "#module.#config"

// helloDefaultMessage is the fixture module's #config.message default
// (test/fixtures/modules/hello/module.cue).
const helloDefaultMessage = "hello from opm"

// configMapMessage returns data.message of the ConfigMap the fixture module
// renders, failing the spec when the result carries no ConfigMap.
func configMapMessage(res *render.RenderResult) string {
	GinkgoHelper()
	for _, r := range res.Resources {
		if r.Kind() != "ConfigMap" {
			continue
		}
		u, err := r.ToUnstructured()
		Expect(err).NotTo(HaveOccurred())
		msg, found, err := unstructured.NestedString(u.Object, "data", "message")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue(), "the fixture's ConfigMap carries data.message")
		return msg
	}
	Fail("the fixture module renders no ConfigMap")
	return ""
}

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
			store = generatedPlatformStore(k, registry)
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
			Expect(res.ResolvedVersions).NotTo(BeEmpty(), "the build reports the resolved-versions rows (0019:D18)")
			Expect(res.ModuleVersion).To(Equal(hello.Version),
				"the render reports the version the module declares, as bare SemVer")
			Expect(res.PlatformIdentity).To(Equal(store.Identity().String()),
				"the render reports the identity of the package it built against (0015:D13)")
			Expect(res.SkewPolicy).To(Equal(releasesv1alpha1.SkewPolicyWarn),
				"the render reports the default skew policy of the record it leased")
			Expect(store.Leased()).To(BeEmpty(), "the render releases its lease on return")
			Expect(configMapMessage(res)).To(Equal("kernel hello"), "the supplied values reach the rendered object")

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

		It("reports the superseded identity when a newer package lands mid-render", func() {
			renderer := &render.KernelModuleRenderer{
				Kernel:      k,
				Store:       store,
				Registry:    registry,
				RuntimeName: core.LabelManagedByControllerValue,
			}

			consumed, ok := store.Generated()
			Expect(ok).To(BeTrue())

			type outcome struct {
				res *render.RenderResult
				err error
			}
			results := make(chan outcome, 1)
			hello := fixtures.Must(GinkgoT(), "hello")
			go func() {
				defer GinkgoRecover()
				res, err := renderer.RenderModule(ctx,
					"kernel-hello-identity", "default",
					hello.ModulePath, hello.Tag(),
					nil)
				results <- outcome{res: res, err: err}
			}()

			// Supersede the package once the render has taken its lease, or
			// once it has already returned: either way it is committed to the
			// package it started against, which is the whole point of the
			// lease.
			Eventually(func() bool {
				return len(store.Leased()) > 0 || len(results) > 0
			}).Should(BeTrue(), "the render should lease the package it builds against")
			superseding := consumed
			superseding.Identity = platformstore.NewPackageIdentity(
				consumed.Identity.Generation(),
				[]platformstore.ClaimCoordinate{{Catalog: "opmodel.dev/catalogs/k8up@v1", Version: "1.0.0"}},
			)
			store.SetGenerated(superseding)

			out := <-results
			Expect(out.err).NotTo(HaveOccurred())
			Expect(store.Identity()).To(Equal(superseding.Identity), "a newer package is current")
			Expect(out.res.PlatformIdentity).To(Equal(consumed.Identity.String()),
				"a render is attributable to the exact registry state it consumed")
			Expect(out.res.PlatformIdentity).NotTo(Equal(superseding.Identity.String()))
		})

		// The instance side of the removal guard (0015:D3, D16).
		// The assertion is on the exact FQN sets rather than on "not empty":
		// the guard's whole correctness is that these strings are the same
		// keyspace TransformerRegistration.spec.provides carries, so a
		// derivation that produced plausible-looking keys of another shape
		// would pass a weaker check and count no dependents in production.
		DescribeTable("reports the contracts the instance's components declare",
			func(fixtureName string, want []string) {
				renderer := &render.KernelModuleRenderer{
					Kernel:      k,
					Store:       store,
					Registry:    registry,
					RuntimeName: core.LabelManagedByControllerValue,
				}

				f := fixtures.Must(GinkgoT(), fixtureName)
				res, err := renderer.RenderModule(ctx,
					"demand-"+strings.ReplaceAll(fixtureName, "_", "-"), "default",
					f.ModulePath, f.Tag(), nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(res.RequiredContracts).To(Equal(want))
			},
			Entry("a single-resource module", "hello", []string{
				"opmodel.dev/catalogs/opm/resources/config-maps@v1beta1",
			}),
			Entry("a workload module with traits", "hello_web", []string{
				"opmodel.dev/catalogs/opm/resources/container@v1beta1",
				"opmodel.dev/catalogs/opm/traits/init-containers@v1beta1",
				"opmodel.dev/catalogs/opm/traits/restart-policy@v1beta1",
				"opmodel.dev/catalogs/opm/traits/scaling@v1beta1",
				"opmodel.dev/catalogs/opm/traits/sidecar-containers@v1beta1",
				"opmodel.dev/catalogs/opm/traits/update-strategy@v1beta1",
			}),
		)

		It("takes the module's #config defaults when no values are supplied", func() {
			renderer := &render.KernelModuleRenderer{
				Kernel:      k,
				Store:       store,
				Registry:    registry,
				RuntimeName: core.LabelManagedByControllerValue,
			}

			hello := fixtures.Must(GinkgoT(), "hello")
			res, err := renderer.RenderModule(ctx,
				"kernel-hello-defaults", "default",
				hello.ModulePath, hello.Tag(),
				nil)

			Expect(err).NotTo(HaveOccurred(), "an empty values stack leaves #config to its defaults")
			Expect(res).NotTo(BeNil())
			Expect(configMapMessage(res)).To(Equal(helloDefaultMessage))
		})

		// What a user reads when spec.values violate the module's #config.
		// The frame and path constants are the parts that move when the
		// renderer stops checking the values itself; the findings and their
		// spec.values positions stay.
		DescribeTable("reports a #config violation at the spec.values origin",
			func(raw string, wantFindings ...string) {
				renderer := &render.KernelModuleRenderer{
					Kernel:      k,
					Store:       store,
					Registry:    registry,
					RuntimeName: core.LabelManagedByControllerValue,
				}

				// The values reach synthesis as one source whose origin names
				// the CR field, so the error is attributed there rather than to
				// an anonymous values filename.
				values := &releasesv1alpha1.RawValues{}
				values.Raw = []byte(raw)
				hello := fixtures.Must(GinkgoT(), "hello")
				res, err := renderer.RenderModule(ctx,
					"kernel-hello-bad-values", "default",
					hello.ModulePath, hello.Tag(),
					values)

				Expect(res).To(BeNil())
				Expect(err).To(HaveOccurred())
				GinkgoWriter.Printf("values %s: %s\n", raw, err)
				Expect(err.Error()).To(HavePrefix(valuesFrame("kernel-hello-bad-values")),
					"a values error carries the renderer's frame")
				for _, want := range wantFindings {
					Expect(err.Error()).To(ContainSubstring(want))
				}
				Expect(store.Leased()).To(BeEmpty(), "a failed render releases its lease on return")
			},
			Entry("a value of the wrong type", `{"message": 42}`,
				configPath+".message: conflicting values 42 and string (",
				"spec.values:1:1, spec.values:1:13)"),
			Entry("a field #config does not allow", `{"bogus": 1}`,
				"field not allowed (spec.values:1:2)"),
		)

		// A required #config value left unset: the required_values fixture
		// declares message (read by its component) and count (read by
		// nothing), neither with a default.
		Context("when the values leave a required #config value unset", func() {
			var renderer *render.KernelModuleRenderer
			var fixture fixtures.Coordinate

			BeforeEach(func() {
				renderer = &render.KernelModuleRenderer{
					Kernel:      k,
					Store:       store,
					Registry:    registry,
					RuntimeName: core.LabelManagedByControllerValue,
				}
				fixture = fixtures.Must(GinkgoT(), "required_values")
			})

			renderWith := func(name, raw string) (*render.RenderResult, error) {
				values := &releasesv1alpha1.RawValues{}
				values.Raw = []byte(raw)
				return renderer.RenderModule(ctx, name, "default", fixture.ModulePath, fixture.Tag(), values)
			}

			// Synthesis checks the values against #config without
			// concreteness and then the whole built instance for it, so an
			// unset value is reported where a component reads it.
			It("refuses an unset value a component reads, at the component path", func() {
				res, err := renderWith("required-read", `{"count": 1}`)

				Expect(res).To(BeNil())
				Expect(err).To(HaveOccurred())
				GinkgoWriter.Printf("unset and read: %s\n", err)
				Expect(err.Error()).To(HavePrefix(valuesFrame("required-read")))
				Expect(err.Error()).To(ContainSubstring(
					"not fully concrete: components.required.spec.configMaps.required.data.message: incomplete value string"))
				Expect(errors.Is(err, render.ErrAcquire)).To(BeFalse(), "the refusal is not an acquisition failure")
			})

			It("renders when an unset value is read by nothing", func() {
				res, err := renderWith("required-unread", `{"message": "x"}`)

				Expect(err).NotTo(HaveOccurred(), "nothing in the build consumes count")
				Expect(configMapMessage(res)).To(Equal("x"))
			})
		})

		// The same violation through the ModuleInstance reconciler: the
		// condition and reason a user sees, with the renderer's error as the
		// condition message.
		It("stalls a ModuleInstance whose spec.values violate #config with reason RenderFailed", func() {
			renderer := &render.KernelModuleRenderer{
				Kernel:      k,
				Store:       store,
				Registry:    registry,
				RuntimeName: core.LabelManagedByControllerValue,
			}
			reconciler := &opmcontroller.ModuleInstanceReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				RestConfig:      cfg,
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(16),
				Renderer:        renderer,
			}

			hello := fixtures.Must(GinkgoT(), "hello")
			values := &releasesv1alpha1.RawValues{}
			values.Raw = []byte(`{"message": 42}`)
			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "values-violation-mi", Namespace: "default"},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: hello.ModulePath, Version: hello.Tag()},
					Values: values,
				},
			}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, mi)).To(Succeed())
				_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: keyOfObject(mi)})
			})

			req := reconcile.Request{NamespacedName: keyOfObject(mi)}
			_, err := reconciler.Reconcile(ctx, req) // adds the finalizer
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred(), "a stalled failure returns no error")

			var current releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, keyOfObject(mi), &current)).To(Succeed())

			// The message the renderer returns for the values as stored, rendered
			// directly, is what the condition carries unchanged. The API server
			// stores spec.values as compact JSON, so the positions are those of
			// the stored document, not of the bytes the test sent.
			_, renderErr := renderer.RenderModule(ctx, mi.Name, mi.Namespace, hello.ModulePath, hello.Tag(), current.Spec.Values)
			Expect(renderErr).To(HaveOccurred())
			GinkgoWriter.Printf("condition message: %s\n", renderErr)
			for _, cond := range []string{status.ReadyCondition, status.StalledCondition} {
				c := meta.FindStatusCondition(current.Status.Conditions, cond)
				Expect(c).NotTo(BeNil(), "%s is set", cond)
				Expect(c.Reason).To(Equal(status.RenderFailedReason), "%s reason", cond)
				Expect(c.Message).To(Equal(renderErr.Error()), "%s message is the renderer's error", cond)
			}
			Expect(meta.IsStatusConditionFalse(current.Status.Conditions, status.ReadyCondition)).To(BeTrue())
			Expect(meta.IsStatusConditionTrue(current.Status.Conditions, status.StalledCondition)).To(BeTrue())
			Expect(current.Status.Inventory).To(BeNil(), "nothing is applied under a refused render")
		})
	})
})
