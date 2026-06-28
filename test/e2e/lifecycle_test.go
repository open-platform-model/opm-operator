//go:build e2e
// +build e2e

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

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Lifecycle", func() {
	// TODO (design 5.1): Validates full create → Ready → update → delete flow
	// against a deployed controller. Requires: deployed controller in Kind,
	// create OCIRepository + ModuleInstance → Eventually Ready=True → managed
	// resources exist → patch spec.values → Eventually resources reflect new
	// values → delete ModuleInstance → Eventually managed resources cleaned up,
	// finalizer removed, CR gone.
	It("should complete create, update, and delete lifecycle", func() {
		Skip("TODO: requires deployed controller and end-to-end fixture wiring")
	})

	// TODO (design 5.2): Validates the real OCI fetch pipeline against a live
	// artifact server. Requires: push OCI artifact to the Kind-local registry →
	// OCIRepository pointing at it → ModuleInstance → Eventually controller
	// performs HTTP fetch, digest verification, zip extraction, CUE validation
	// → Ready=True, managed resources reflect artifact contents.
	It("should fetch and render a real OCI artifact", func() {
		Skip("TODO: requires in-cluster OCI registry and artifact push fixture")
	})
})
