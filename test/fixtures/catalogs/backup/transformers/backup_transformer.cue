package transformers

import (
	"encoding/json"

	id "testing.opmodel.dev/catalogs/operator/backup/identity"
	c "opmodel.dev/core@v2"
	tra "opmodel.dev/catalogs/opm/traits/v1alpha1"
)

// WHY this transformer is the catalog's whole point: opm's backup trait is
// provider-fulfilled (opm declares it and ships no transformer for it), so a
// platform renders a component carrying it only once a provider catalog is in
// its registry. Requiring the trait here is what makes `provides` of a claim
// naming this catalog exactly opmodel.dev/catalogs/opm/traits/backup@v1alpha1.

// #BackupTransformer renders a component's backup policy as one ConfigMap. A
// real engine would render its own schedule object; a ConfigMap needs no CRD,
// so the fixture works on any cluster the operator runs on.
#BackupTransformer: c.#ComponentTransformer & {
	metadata: {
		modulePath:     id.kindPrefix.transformers
		name:           "backup-transformer"
		catalogVersion: id.Version
		fqn:            "\(id.kindPrefix.transformers)/backup-transformer@\(id.Version)"
		description:    "Renders opm's backup trait as one ConfigMap holding the policy"
	}

	requiredLabels: {}
	requiredResources: {}
	optionalResources: {}
	requiredTraits: {
		(tra.#BackupTrait.metadata.fqn): tra.#BackupTrait
	}
	optionalTraits: {}

	#transform: {
		#component: _
		#context:   c.#TransformerContext

		_policy: #component.spec.backup

		output: {
			apiVersion: "v1"
			kind:       "ConfigMap"
			metadata: {
				name:      "\(#component.#names.resourceName)-backup"
				namespace: #context.#moduleInstanceMetadata.namespace
				labels:    #context.labels
			}
			data: {
				schedule:  _policy.schedule
				retention: json.Marshal(_policy.retention)
			}
		}
	}
}
