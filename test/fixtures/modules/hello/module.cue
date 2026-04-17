package hello

import (
	m "opmodel.dev/core/v1alpha1/module@v1"
)

m.#Module

metadata: {
	modulePath:       "opmodel.dev/test"
	name:             "hello"
	version:          "0.0.1"
	description:      "Minimal test module — renders a single ConfigMap"
	defaultNamespace: "default"
}

#config: {
	message: string | *"hello from opm"
}

debugValues: {
	message: "hello from opm (debug)"
}
