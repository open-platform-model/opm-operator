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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cmd suite")
}

var _ = Describe("resolveRegistry", func() {
	Context("flag and env interaction", func() {
		It("returns the flag value when the flag is non-empty", func() {
			GinkgoT().Setenv("OPM_REGISTRY", "env-value")
			Expect(resolveRegistry("flag-value")).To(Equal("flag-value"))
		})

		It("falls back to OPM_REGISTRY when the flag is empty", func() {
			GinkgoT().Setenv("OPM_REGISTRY", "env-value")
			Expect(resolveRegistry("")).To(Equal("env-value"))
		})

		It("returns an empty string when both flag and env are empty", func() {
			GinkgoT().Setenv("OPM_REGISTRY", "")
			Expect(resolveRegistry("")).To(BeEmpty())
		})

		It("prefers the flag over the env var when both are set", func() {
			GinkgoT().Setenv("OPM_REGISTRY", "env-value")
			Expect(resolveRegistry("flag-wins")).To(Equal("flag-wins"))
		})
	})
})
