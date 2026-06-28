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
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Reconcile Change Propagation", func() {
	// TODO (design 1.1): Validates Phase 4 no-op path when spec.values change.
	// Requires: initial reconcile creating the managed ConfigMap → patch
	// ModuleInstance spec.values → second reconcile → assert config digest differs,
	// Phase 3 re-renders, Phase 5 re-applies, ConfigMap data.message reflects the
	// new value, inventory revision bumps, history records two success entries.
	It("should re-apply when spec.values changes", func() {
		Skip("TODO: implement values-change propagation across two reconcile cycles")
	})

	// TODO (design 1.2): Validates Phase 1 source resolve on artifact revision
	// change. Requires: initial reconcile with an OCIRepository reporting revision
	// A → mutate artifact revision to B on the OCIRepository status →
	// second reconcile → assert new source digest, full pipeline re-executes,
	// lastAppliedSourceDigest updates, managed resource reflects new content.
	It("should re-apply when source revision changes", func() {
		Skip("TODO: implement source-revision propagation across two reconcile cycles")
	})
})
