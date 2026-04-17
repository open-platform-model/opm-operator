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

var _ = Describe("Prune", func() {
	// TODO: Validates that stale resources are pruned during a full reconcile cycle.
	// Requires: ModuleRelease fixture → initial reconcile populating inventory →
	// update removing a resource from the desired set → re-reconcile → verify
	// the removed resource is deleted from the cluster.
	It("should prune stale resources after module release update", func() {
		Skip("TODO: requires ModuleRelease reconcile cycle with inventory tracking")
	})

	// TODO: Validates the Namespace safety exclusion during a real reconcile.
	// Requires: a reconcile cycle that produces a Namespace in the stale set,
	// verifying the controller logs a warning and retains the Namespace.
	It("should skip Namespace resources during pruning", func() {
		Skip("TODO: requires end-to-end reconcile producing Namespace in stale set")
	})

	// TODO: Validates the CRD safety exclusion during a real reconcile.
	// Requires: a reconcile cycle that produces a CRD in the stale set,
	// verifying the controller logs a warning and retains the CRD.
	It("should skip CustomResourceDefinition resources during pruning", func() {
		Skip("TODO: requires end-to-end reconcile producing CRD in stale set")
	})

	// TODO: Validates correct counting when the stale set contains both
	// pruneable and excluded resources during a full reconcile cycle.
	// Requires: a reconcile producing both deletable and excluded resources
	// in the stale set, verifying deleted/skipped counts in controller metrics or logs.
	It("should handle mixed stale set with both pruneable and excluded resources", func() {
		Skip("TODO: requires end-to-end reconcile producing mixed stale set")
	})

	// TODO: Validates that spec.prune=false prevents pruning entirely.
	// Requires: a ModuleRelease with spec.prune=false, an update removing
	// a resource from the desired set, re-reconcile, and verify the stale
	// resource is retained in the cluster.
	It("should skip pruning when spec.prune is false", func() {
		Skip("TODO: requires ModuleRelease with spec.prune=false")
	})
})
