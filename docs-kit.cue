// The opm-operator docs bundle, built by docs-kit's opm-docs (docs-kit
// docs/contracts.md C6, C15, C18) and published by docs.yml and release.yml.
bundles: "opm-operator": {
	placement: {kind: "docs", root: "/docs/", owns: ["reference/operator-resources.md"]}
	version: {from: "tag", prefix: "v"}
	sources: [
		// The authored pages ship in the same bundle (docs-kit DESIGN decision
		// 20). Until the site reads this bundle, the committed page carries
		// crdref's block, which would collide with the crd page, so it is
		// excluded; after the site switch the block and this exclude go, and
		// the authored intro then completes the generated page.
		{kind: "markdown", dir: "docs/site", exclude: ["reference/operator-resources.md"]},
		{
			kind:        "crd"
			dir:         "./config/crd/bases"
			samples:     "./config/samples"
			page:        "reference/operator-resources.md"
			title:       "Operator resources"
			description: "One generated entry per operator resource kind: ModuleInstance, ModulePackage, Platform and TransformerRegistration."
			weight:      7 // the committed page's weight, so the page keeps its place before the intro completes it
			order: ["ModuleInstance", "ModulePackage", "Platform", "TransformerRegistration"]
			// The hello fixture is a test module, not an example to copy.
			hideSamplesMatching: ["testing.opmodel.dev"]
			// kubebuilder's scaffold labels on every sample; removed only when the
			// value matches.
			stripLabels: {
				"app.kubernetes.io/name":       "opm-operator"
				"app.kubernetes.io/managed-by": "kustomize"
			}
			// The Named(...) of the controller whose builder calls For(&<Kind>{})
			// in internal/controller. Rename a controller, edit this map:
			// internal/controller/docskit_reconciledby_test.go fails until they agree.
			reconciledBy: {
				ModuleInstance:          "moduleinstance"
				ModulePackage:           "modulepackage"
				Platform:                "platform"
				TransformerRegistration: "transformerregistration"
			}
			citations: "link"
		},
	]
}
