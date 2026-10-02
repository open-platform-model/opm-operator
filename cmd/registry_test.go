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

package main

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cmd suite")
}

var _ = Describe("resolveRegistry", func() {
	DescribeTable("precedence",
		func(flagValue string, env *string, wantRegistry, wantSource string) {
			if env != nil {
				GinkgoT().Setenv("OPM_REGISTRY", *env)
			} else {
				// Setenv registers cleanup that restores the original value.
				GinkgoT().Setenv("OPM_REGISTRY", "")
				Expect(os.Unsetenv("OPM_REGISTRY")).To(Succeed())
			}
			gotRegistry, gotSource := resolveRegistry(flagValue)
			Expect(gotRegistry).To(Equal(wantRegistry))
			Expect(gotSource).To(Equal(wantSource))
		},
		Entry("flag set, env set: flag wins", "flag-value", new("env-value"), "flag-value", registrySourceFlag),
		Entry("flag set, env unset: flag wins", "flag-value", nil, "flag-value", registrySourceFlag),
		Entry("flag empty, env set: env wins", "", new("env-value"), "env-value", registrySourceEnv),
		Entry("flag empty, env empty: built-in default", "", new(""), defaultRegistry, registrySourceDefault),
		Entry("flag empty, env unset: built-in default", "", nil, defaultRegistry, registrySourceDefault),
	)

	It("keeps GHCR as the built-in default so a stock install resolves opmodel.dev", func() {
		Expect(defaultRegistry).To(ContainSubstring("opmodel.dev=ghcr.io/open-platform-model"))
		Expect(defaultRegistry).To(ContainSubstring("testing.opmodel.dev=ghcr.io/open-platform-model"))
	})
})
