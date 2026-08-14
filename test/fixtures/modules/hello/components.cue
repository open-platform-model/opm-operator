package hello

import (
	res "opmodel.dev/catalogs/opm/resources/v1beta1"
)

#components: {
	hello: {
		res.#ConfigMaps

		metadata: name: "hello"

		spec: configMaps: {
			"hello": {
				data: message: #config.message
			}
		}
	}
}
