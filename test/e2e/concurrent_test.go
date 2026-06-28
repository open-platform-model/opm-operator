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

var _ = Describe("Concurrent Reconciliation", func() {
	// TODO (design 6.1): Validates multiple ModuleInstances against a shared
	// source reconcile correctly under concurrency. Requires: deployed
	// controller, shared OCIRepository, create 3 ModuleInstances referencing it
	// → Eventually all report Ready=True → assert each has its own distinct
	// managed resources and no cross-release contamination in inventories.
	It("should reconcile multiple ModuleInstances from a shared source", func() {
		Skip("TODO: requires concurrent reconcile fixture across parallel ModuleInstances")
	})

	// TODO (design 6.2): Validates idempotency when the controller restarts
	// mid-reconcile. Requires: create ModuleInstance → wait for reconcile to
	// start → delete controller pod (kubectl delete pod -l ...) → Eventually
	// pod restarts and Ready=True → assert no duplicate resources, inventory
	// matches render output, no orphaned resources.
	It("should converge correctly after a controller restart mid-reconcile", func() {
		Skip("TODO: requires controller-pod kill and restart fixture")
	})
})
