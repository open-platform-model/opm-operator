// required_values — test module whose #config declares two required values
// with no defaults: message, which its one component reads, and count, which
// nothing reads. Renders a single ConfigMap. Consumed by the operator's
// registry-backed integration tests, which pin what a ModuleInstance reports
// when its values leave a required field unset.
package required_values

import (
	"strings"

	m "opmodel.dev/core@v2"

	id "testing.opmodel.dev/modules/operator/required_values/identity"
)

m.#Module

// Module metadata — modulePath and version are the identity package's values,
// and name is the path's leaf (0010:D8, 0011:D12). Edit
// identity/identity.cue, not this block.
metadata: {
	_segments:   strings.Split(strings.SplitN(id.ModulePath, "@", 2)[0], "/")
	name:        _segments[len(_segments)-1]
	modulePath:  id.ModulePath
	version:     id.Version
	description: "Test module with required #config values and no defaults — renders a single ConfigMap"
}

#config: {
	// Read by the component below.
	message: string
	// Read by nothing.
	count: int
}

debugValues: {
	message: "required values (debug)"
	count:   1
}
