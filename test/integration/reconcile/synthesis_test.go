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

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/poc-controller/internal/synthesis"
	"github.com/open-platform-model/poc-controller/pkg/loader"
)

// These tests require the fixture module published to a local OCI registry.
// They are skipped automatically in CI (ghcr has no fixture module).
// Run with: task dev:test:local

var _ = Describe("Release Synthesis Integration", func() {
	Context("when OCI registry has the test module published", func() {
		BeforeEach(func() {
			skipIfNoTestRegistry()
		})

		It("should synthesize, load, and produce concrete components", func() {
			dir, err := synthesis.SynthesizeRelease(synthesis.ReleaseParams{
				Name:          "test-hello",
				Namespace:     "default",
				ModulePath:    "testing.opmodel.dev/test/hello@v0",
				ModuleVersion: "v0.0.1",
			})
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()

			cueCtx := cuecontext.New()
			val, err := loader.LoadModulePackage(cueCtx, dir)
			Expect(err).NotTo(HaveOccurred())

			// Verify components field exists (materialized from #components by #ModuleRelease).
			components := val.LookupPath(cue.ParsePath("components"))
			Expect(components.Exists()).To(BeTrue(), "components field must exist in loaded #ModuleRelease")
			Expect(components.Err()).NotTo(HaveOccurred(), "components must not have evaluation errors")

			// Verify at least one component is present.
			iter, err := components.Fields()
			Expect(err).NotTo(HaveOccurred())
			count := 0
			for iter.Next() {
				count++
			}
			Expect(count).To(BeNumerically(">", 0), "components must have at least one entry")

			// Verify the hello component exists specifically.
			hello := components.LookupPath(cue.ParsePath("hello"))
			Expect(hello.Exists()).To(BeTrue(), "hello component must exist")
		})

		It("should produce metadata and #module binding from synthesis params", func() {
			dir, err := synthesis.SynthesizeRelease(synthesis.ReleaseParams{
				Name:          "meta-test",
				Namespace:     "test-ns",
				ModulePath:    "testing.opmodel.dev/test/hello@v0",
				ModuleVersion: "v0.0.1",
			})
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()

			cueCtx := cuecontext.New()
			val, err := loader.LoadModulePackage(cueCtx, dir)
			Expect(err).NotTo(HaveOccurred())

			// Verify metadata.name is populated from synthesis params.
			nameVal := val.LookupPath(cue.ParsePath("metadata.name"))
			Expect(nameVal.Exists()).To(BeTrue())
			name, err := nameVal.String()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("meta-test"))

			// Verify metadata.namespace.
			nsVal := val.LookupPath(cue.ParsePath("metadata.namespace"))
			Expect(nsVal.Exists()).To(BeTrue())
			ns, err := nsVal.String()
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal("test-ns"))

			// Verify #module definition is bound to the imported module.
			moduleDef := val.LookupPath(cue.MakePath(cue.Def("module")))
			Expect(moduleDef.Exists()).To(BeTrue(), "#module must be bound to imported module")
		})
	})
})
