/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/open-platform-model/opm-operator/internal/catalog"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/synthesis"
	opmcore "github.com/open-platform-model/opm-operator/pkg/core"
	"github.com/open-platform-model/opm-operator/pkg/loader"
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// Render-and-check contract test: locks the contract between the Go constant
// `core.LabelManagedByControllerValue` and the CUE catalog's
// `#TransformerContext.#runtimeName` field. Drift on either side breaks this test.
var _ = Describe("Runtime identity injection", Ordered, func() {
	var realProv *provider.Provider

	BeforeEach(func() {
		skipIfNoTestRegistry()
	})

	BeforeAll(func() {
		skipIfNoTestRegistry()
		p, err := catalog.LoadProvider("../../../catalog", "kubernetes")
		if err != nil {
			Skip(fmt.Sprintf("catalog.LoadProvider failed — "+
				"ensure opmodel.dev catalog modules are published to the local registry: %v", err))
		}
		realProv = p
	})

	It("stamps managed-by = opm-controller and propagates release uuid on rendered resources", func() {
		renderer := &render.RegistryRenderer{}
		result, err := renderer.RenderModule(ctx,
			"runtime-ident-hello",
			"default",
			"testing.opmodel.dev/modules/hello@v0",
			"v0.0.1",
			nil,
			realProv,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Resources).NotTo(BeEmpty(),
			"at least one resource must render")

		for _, res := range result.Resources {
			u, err := res.ToUnstructured()
			Expect(err).NotTo(HaveOccurred())

			labels := u.GetLabels()
			Expect(labels).NotTo(BeNil(),
				"rendered resource %s must carry labels", u.GetName())

			Expect(labels[opmcore.LabelManagedBy]).To(Equal(opmcore.LabelManagedByControllerValue),
				"managed-by must be opm-controller (Go/CUE contract)")
			Expect(labels[opmcore.LabelModuleReleaseUUID]).NotTo(BeEmpty(),
				"module-release uuid must be non-empty (catalog ownership labels must continue to flow)")
		}
	})

	It("fails CUE evaluation when #runtimeName is unset on #TransformerContext", func() {
		// Synthesize a temporary CUE package that imports the catalog's
		// #TransformerContext and unifies it without filling #runtimeName.
		// The catalog declares #runtimeName as mandatory; concrete evaluation
		// must surface a missing-required-field error.
		dir, err := os.MkdirTemp("", "opm-runtime-ident-neg-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(dir) })

		mustWrite := func(rel, body string) {
			full := filepath.Join(dir, rel)
			Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
			Expect(os.WriteFile(full, []byte(body), 0o644)).To(Succeed())
		}

		mustWrite("cue.mod/module.cue", fmt.Sprintf(`module: "opmodel.dev/test/runtime-ident-neg@v0"
language: version: %q
deps: "opmodel.dev/core/v1alpha1@v1": v: %q
`, synthesis.CUELanguageVersion, synthesis.CatalogVersion))

		mustWrite("check.cue", `package check

import tf "opmodel.dev/core/v1alpha1/transformer@v1"

ctx: tf.#TransformerContext & {
	#moduleReleaseMetadata: {
		name:      "rel"
		namespace: "ns"
		uuid:      "00000000-0000-0000-0000-000000000000"
	}
	#componentMetadata: {
		name: "c"
	}
	// #runtimeName intentionally omitted — must fail evaluation
}
`)

		cueCtx := cuecontext.New()
		val, err := loader.LoadModulePackage(cueCtx, dir)
		Expect(err).NotTo(HaveOccurred(), "CUE module package must load")

		ctxVal := val.LookupPath(cue.ParsePath("ctx"))
		verr := ctxVal.Validate(cue.Concrete(true))
		Expect(verr).To(HaveOccurred(),
			"missing #runtimeName must surface as a validation error")
		Expect(verr.Error()).To(ContainSubstring("runtimeName"),
			"error must mention the missing field name")
	})
})
