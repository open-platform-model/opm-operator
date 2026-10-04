// backup_consumer — the consumer half of the operator's registration fixture
// set. Its one component attaches opm's backup trait, a provider-fulfilled
// contract that no catalog the operator's sample platform subscribes
// implements. Its render is refused naming the contract until a provider is
// active (0010:D28); once backup_provider's claim is accepted and active it
// renders one ConfigMap through the backup catalog fixture's transformer.
package backup_consumer

import (
	"strings"

	m "opmodel.dev/core@v2"

	id "testing.opmodel.dev/modules/operator/backup_consumer/identity"
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
	description: "Consumer test module — demands opm's provider-fulfilled backup trait"
}

#config: {
	// When the backup runs, as a five-field cron expression.
	schedule: string | *"0 3 * * *"
	// How many daily captures to keep.
	keepDaily: int & >0 | *7
}

debugValues: {
	schedule:  "30 2 * * *"
	keepDaily: 3
}
