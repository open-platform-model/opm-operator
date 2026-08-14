// hello — minimal core-v2 (opmodel.dev/core@v2) test module. One component
// attaches the catalog's ConfigMaps resource, which the catalog's
// configmap-transformer matches without any workload-type label
// (requiredLabels: {}). Renders a single ConfigMap. Consumed by the operator's
// registry-backed integration tests.
package hello

import (
	m "opmodel.dev/core@v2"
)

m.#Module

metadata: {
	name:        "hello"
	modulePath:  "opmodel.dev/modules/test/hello@v0"
	version:     "0.0.5"
	description: "Minimal test module — renders a single ConfigMap"
}

#config: {
	message: string | *"hello from opm"
}

debugValues: {
	message: "hello from opm (debug)"
}
