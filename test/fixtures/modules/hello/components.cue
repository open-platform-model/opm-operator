package hello

import (
	resources_config "opmodel.dev/opm/v1alpha1/resources/config@v1"
)

#components: {
	hello: {
		resources_config.#ConfigMaps

		metadata: {
			name: "hello"
			labels: "core.opmodel.dev/workload-type": "config"
		}

		spec: configMaps: {
			"hello": {
				data: message: #config.message
			}
		}
	}
}
