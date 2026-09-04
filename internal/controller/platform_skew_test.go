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

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// The skew policy the reconciler records beside the generated module (0019
// D7/D18): Refuse maps to the kernel's SkewRefuse, everything else (Warn,
// unset) to the SkewWarn default. The CRD enum keeps other values out.
var _ = Describe("Platform skew policy resolution", func() {
	ptr := func(s string) *string { return &s }

	DescribeTable("maps spec.skewPolicy to the kernel policy",
		func(policy *string, want kernel.SkewPolicy) {
			plat := &releasesv1alpha1.Platform{Spec: releasesv1alpha1.PlatformSpec{SkewPolicy: policy}}
			Expect(skewPolicy(plat)).To(Equal(want))
		},
		Entry("unset resolves to Warn", nil, kernel.SkewWarn),
		Entry("Warn", ptr(releasesv1alpha1.SkewPolicyWarn), kernel.SkewWarn),
		Entry("Refuse", ptr(releasesv1alpha1.SkewPolicyRefuse), kernel.SkewRefuse),
	)
})
