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
	"flag"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("validateDriftRenderInterval", func() {
	DescribeTable("accepts zero and positive intervals, refuses negative ones",
		func(d time.Duration, wantErr bool) {
			err := validateDriftRenderInterval(d)
			if wantErr {
				Expect(err).To(MatchError(ContainSubstring("--drift-render-interval")))
				return
			}
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("the default", 30*time.Minute, false),
		Entry("zero disables the skip", time.Duration(0), false),
		Entry("negative", -time.Minute, true),
	)
})

var _ = Describe("validateRenderTimeout", func() {
	DescribeTable("accepts zero and positive timeouts, refuses negative ones",
		func(d time.Duration, wantErr bool) {
			err := validateRenderTimeout(d)
			if wantErr {
				Expect(err).To(MatchError(ContainSubstring("--render-timeout")))
				return
			}
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("the default", defaultRenderTimeout, false),
		Entry("zero disables the deadline", time.Duration(0), false),
		Entry("negative", -time.Second, true),
	)
})

var _ = Describe("--render-timeout", func() {
	It("defaults to ten minutes", func() {
		Expect(defaultRenderTimeout).To(Equal(10 * time.Minute))

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		var d time.Duration
		registerRenderTimeoutFlag(fs, &d)
		Expect(fs.Lookup("render-timeout").DefValue).To(Equal("10m0s"))
		Expect(fs.Parse(nil)).To(Succeed())
		Expect(d).To(Equal(10 * time.Minute))
	})
})
