// backup — the operator's provider catalog fixture: the smallest catalog that
// implements a provider-fulfilled contract. Its one transformer implements
// opm's backup trait (opmodel.dev/catalogs/opm/traits/backup@v1alpha1), which
// opm declares and nothing else in the workspace implements, so a
// TransformerRegistration naming this catalog provides exactly that contract
// (0015:D11). The backup_provider module fixture renders that claim, and the
// backup_consumer module fixture demands the contract.
//
// WHY a catalog of its own and not a member of the provider catalog fixture:
// `task deps:cascade` advances the provider catalog's version on every core
// move, and a claim names one catalog build. The claim is authored in the
// backup_provider module, which cannot import a catalog fixture version that
// is new in the same pull request (`hack/fixtures.sh check` dry-runs every
// fixture against GHCR before the tree is seeded), so it would name a build
// the PR's job-local registry no longer holds. The cascade leaves this
// catalog alone; its pins only ever trail the platform's, which the build
// compatibility check accepts.
package backup

import (
	c "opmodel.dev/core@v2"
	id "testing.opmodel.dev/catalogs/operator/backup/identity"
	t "testing.opmodel.dev/catalogs/operator/backup/transformers"
)

c.#Catalog
metadata: {
	modulePath:  id.ModulePath
	version:     id.Version
	description: "Operator test catalog: a provider for opm's backup trait"
}

#transformers: {
	(t.#BackupTransformer.metadata.fqn): t.#BackupTransformer
}
