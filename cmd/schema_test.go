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
	"errors"

	"cuelang.org/go/cue"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-platform-model/library/opm/kernel"
)

// failingLoader is a schema.Loader stub whose Load always errors, used to
// exercise verifyCoreSchema's fail-fast path without contacting a registry.
type failingLoader struct{ err error }

func (l failingLoader) Load(*cue.Context) (cue.Value, error) { return cue.Value{}, l.err }

// okLoader is a schema.Loader stub that resolves to an empty struct, used to
// exercise verifyCoreSchema's success path without contacting a registry.
type okLoader struct{}

func (okLoader) Load(ctx *cue.Context) (cue.Value, error) { return ctx.CompileString(`{}`), nil }

var _ = Describe("verifyCoreSchema", func() {
	It("returns the loader error when schema resolution fails", func() {
		k := kernel.New(kernel.WithSchemaLoader(failingLoader{err: errors.New("registry unreachable")}))
		v, err := verifyCoreSchema(k)
		Expect(err).To(MatchError(ContainSubstring("registry unreachable")))
		Expect(v).To(BeEmpty())
	})

	It("returns no error when the schema resolves", func() {
		k := kernel.New(kernel.WithSchemaLoader(okLoader{}))
		_, err := verifyCoreSchema(k)
		Expect(err).NotTo(HaveOccurred())
	})
})
