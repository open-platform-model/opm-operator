package required_values

import (
	res "opmodel.dev/catalogs/opm/resources/v1beta1"
)

#components: {
	required: {
		res.#ConfigMaps

		metadata: name: "required"

		spec: configMaps: {
			"required": {
				data: message: #config.message
			}
		}
	}
}
