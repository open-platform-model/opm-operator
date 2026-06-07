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

	"github.com/open-platform-model/library/opm/kernel"

	"github.com/open-platform-model/opm-operator/internal/moduleacquire"
)

// These tests require the fixture module published to a local OCI registry.
// They are skipped automatically in CI (ghcr has no fixture module).
// Run with: task dev:test:local

// countAcquireTempDirs reports how many shim temp dirs (opm-acquire-*) exist
// under the system temp root. Acquire must leave none behind.
func countAcquireTempDirs() int {
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "opm-acquire-*"))
	Expect(err).NotTo(HaveOccurred())
	return len(matches)
}

var _ = Describe("Module Acquisition Integration", func() {
	var k *kernel.Kernel
	var registry string

	BeforeEach(func() {
		skipIfNoTestRegistry()
		registry = os.Getenv("CUE_REGISTRY")
		k = kernel.New(kernel.WithRegistry(registry))
	})

	Context("when the OCI registry has the test module published", func() {
		It("acquires the module and decodes its metadata", func() {
			before := countAcquireTempDirs()

			mod, err := moduleacquire.Acquire(ctx, k,
				"testing.opmodel.dev/modules/hello@v0", "v0.0.1", registry)
			Expect(err).NotTo(HaveOccurred())
			Expect(mod).NotTo(BeNil())
			Expect(mod.Metadata).NotTo(BeNil())
			Expect(mod.Metadata.Name).To(Equal("hello"))
			Expect(mod.Metadata.Version).To(Equal("0.0.1"))

			// No shim temp dir survives a successful acquisition.
			Expect(countAcquireTempDirs()).To(Equal(before))
		})
	})

	Context("when the module path/version is unresolvable", func() {
		It("returns a load error and leaves no temp dir behind", func() {
			before := countAcquireTempDirs()

			mod, err := moduleacquire.Acquire(ctx, k,
				"testing.opmodel.dev/modules/does-not-exist@v0", "v9.9.9", registry)
			Expect(err).To(HaveOccurred())
			Expect(mod).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("acquiring module"))

			// No shim temp dir survives a failed acquisition.
			Expect(countAcquireTempDirs()).To(Equal(before))
		})
	})
})
