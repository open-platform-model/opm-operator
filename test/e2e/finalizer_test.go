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

var _ = Describe("Finalizer and Deletion", func() {
	// TODO: Validates that the controller adds the finalizer to a newly created
	// ModuleRelease during a real reconcile cycle on a Kind cluster.
	// Requires: deploy controller → create ModuleRelease CR → verify
	// metadata.finalizers contains "releases.opmodel.dev/cleanup".
	It("should register finalizer on a new ModuleRelease", func() {
		Skip("TODO: requires deployed controller with real reconcile loop")
	})

	// TODO: Validates full deletion cleanup with a deployed controller.
	// Requires: deploy controller → create ModuleRelease with spec.prune=true →
	// reconcile until Ready → verify managed resources exist → delete the
	// ModuleRelease → verify managed resources are deleted from the cluster
	// and the ModuleRelease object is fully removed (finalizer cleared).
	It("should delete managed resources and complete CR deletion when prune is true", func() {
		Skip("TODO: requires deployed controller with full reconcile + deletion cycle")
	})

	// TODO: Validates that deletion with prune disabled orphans resources.
	// Requires: deploy controller → create ModuleRelease with spec.prune=false →
	// reconcile until Ready → verify managed resources exist → delete the
	// ModuleRelease → verify managed resources are still present on the cluster
	// (orphaned) and the ModuleRelease object is fully removed.
	It("should orphan managed resources when prune is false and CR is deleted", func() {
		Skip("TODO: requires deployed controller with spec.prune=false deletion cycle")
	})

	// TODO: Validates that a suspended ModuleRelease still performs deletion cleanup.
	// Requires: deploy controller → create ModuleRelease → reconcile until Ready →
	// set spec.suspend=true → delete the ModuleRelease → verify managed resources
	// are deleted and CR is fully removed despite suspend being enabled.
	// This confirms suspend only gates normal reconciliation, not object lifecycle.
	It("should perform deletion cleanup even when suspend is true", func() {
		Skip("TODO: requires deployed controller with suspend + deletion interaction")
	})
})
