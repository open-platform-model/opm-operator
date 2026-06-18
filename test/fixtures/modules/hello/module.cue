// hello — minimal kernel-era (opmodel.dev/core@v0) test module. One component
// attaches the catalog's ConfigMaps resource, which the catalog's
// configmap-transformer matches without any workload-type label
// (requiredLabels: {}). Renders a single ConfigMap. Consumed by the operator's
// registry-backed integration tests.
package hello

import (
	m "opmodel.dev/core@v0"
)

m.#Module

metadata: {
	modulePath:  "opmodel.dev/modules/test"
	name:        "hello"
	version:     "0.0.2"
	description: "Minimal test module — renders a single ConfigMap"
}

#config: {
	message: string | *"hello from opm"
}

debugValues: {
	message: "hello from opm (debug)"
}
