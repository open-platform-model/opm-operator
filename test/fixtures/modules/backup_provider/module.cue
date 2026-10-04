// backup_provider — the provider half of the operator's registration fixture
// set. Its one component renders a TransformerRegistration claiming that the
// backup catalog fixture (testing.opmodel.dev/catalogs/operator/backup)
// implements opm's provider-fulfilled backup trait (0015:D3, 0015:D9). Applied
// under an identity that may create the cluster-scoped claim
// (moduleinstance.yaml), the claim is accepted, and it activates once this
// instance is Ready; backup_consumer then renders through the backup catalog's
// transformer.
package backup_provider

import (
	"strings"

	m "opmodel.dev/core@v2"

	id "testing.opmodel.dev/modules/operator/backup_provider/identity"
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
	description: "Provider test module — registers the backup catalog fixture as the backup trait's provider"
}

// Nothing to configure: the claim names one catalog build, which an instance
// must not be able to swap.
#config: {}

debugValues: {}
