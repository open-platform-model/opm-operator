package transformers

import (
	id "testing.opmodel.dev/catalogs/operator/provider/identity"
	c "opmodel.dev/core@v2"
	res "testing.opmodel.dev/catalogs/operator/provider/resources/v1alpha1"
)

// #ProbeTransformer renders a component carrying the probe resource as one
// ConfigMap.
#ProbeTransformer: c.#ComponentTransformer & {
	metadata: {
		modulePath:     id.kindPrefix.transformers
		name:           "probe-transformer"
		catalogVersion: id.Version
		fqn:            "\(id.kindPrefix.transformers)/probe-transformer@\(id.Version)"
		description:    "Renders the probe resource as one ConfigMap"
	}

	requiredLabels: {}
	requiredResources: {
		(res.#ProbeResource.metadata.fqn): res.#ProbeResource
	}
	optionalResources: {}
	requiredTraits: {}
	optionalTraits: {}

	#transform: {
		#component: _
		#context:   c.#TransformerContext

		output: {
			apiVersion: "v1"
			kind:       "ConfigMap"
			metadata: {
				name:      #component.#names.resourceName
				namespace: #context.#moduleInstanceMetadata.namespace
				labels:    #context.labels
			}
			data: #component.spec.probe.data
		}
	}
}
