// hello — minimal core-v2 (opmodel.dev/core@v2) test module. One component
// attaches the catalog's ConfigMaps resource, which the catalog's
// configmap-transformer matches without any workload-type label
// (requiredLabels: {}). Renders a single ConfigMap. Consumed by the operator's
// registry-backed integration tests.
package hello

import (
	"strings"

	m "opmodel.dev/core@v2"

	id "testing.opmodel.dev/modules/operator/hello/identity"
)

m.#Module

// Module metadata — modulePath and version are the identity package's values,
// and name is the path's leaf (enhancements 0010 D8, 0011 D12). Edit
// identity/identity.cue, not this block.
metadata: {
	_segments:   strings.Split(strings.SplitN(id.ModulePath, "@", 2)[0], "/")
	name:        _segments[len(_segments)-1]
	modulePath:  id.ModulePath
	version:     id.Version
	description: "Minimal test module — renders a single ConfigMap"
}

#config: {
	message: string | *"hello from opm"
}

debugValues: {
	message: "hello from opm (debug)"
}
