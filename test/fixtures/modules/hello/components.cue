package hello

import (
	res "opmodel.dev/catalogs/opm/resources"
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
