package backup_provider

import (
	res "opmodel.dev/catalogs/opm/resources/v1alpha1"
	tra "opmodel.dev/catalogs/opm/traits/v1alpha1"
)

#components: {
	// WHY the claim is authored here and not built by #PreBoundRegistration:
	// the pre-bound helper derives the claim from the provider catalog's own
	// package, so this module would import the backup catalog fixture. A
	// module fixture cannot depend on a catalog fixture version that is new in
	// the same pull request (`hack/fixtures.sh check` dry-runs every fixture
	// against GHCR before the tree is seeded), so a catalog bump could never
	// land with its re-pin. The literal is not trusted: acceptance re-derives
	// provides from the named catalog and refuses any difference (0015:D11),
	// and the registry-backed provider fixture spec checks the three fields
	// against the backup catalog fixture's identity and transformers.
	// A bump of test/fixtures/catalogs/backup re-pins `version` here.
	registration: res.#TransformerRegistration & {
		metadata: name: "registration"

		spec: transformerRegistration: {
			catalog: "testing.opmodel.dev/catalogs/operator/backup@v0"
			version: "0.1.0"
			provides: [tra.#BackupTrait.metadata.fqn]
		}
	}
}
