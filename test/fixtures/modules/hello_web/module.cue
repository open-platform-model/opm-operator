// hello_web — minimal container workload (opmodel.dev/core@v2). Renders a
// single Deployment via the catalog's deployment-transformer. Renamed from
// hello-web at the core-v2 crossing: a hyphen violates #SnakeNameType and the
// leaf-equals-name assertion, so name and module path moved together.
package hello_web

import (
	"strings"

	m "opmodel.dev/core@v2"
	res "opmodel.dev/catalogs/opm/resources/v1beta1"

	id "testing.opmodel.dev/modules/operator/hello_web/identity"
)

m.#Module

// Module metadata — modulePath and version are the identity package's values,
// and name is the path's leaf (enhancements 0010 D8, 0011 D12). Edit
// identity/identity.cue, not this block.
metadata: {
	_segments:  strings.Split(strings.SplitN(id.ModulePath, "@", 2)[0], "/")
	name:       _segments[len(_segments)-1]
	modulePath: id.ModulePath
	// Interpolated rather than referenced so the value is concrete before
	// defaults are finalized — the registry loader's shape gate requires a
	// concrete metadata.version, and id.Version is a defaulted disjunction.
	version:     "\(id.Version)"
	description: "Minimal container workload — renders one Deployment"
}

#config: {
	image: res.#Image & {repository: string | *"nginx", tag: string | *"1.27", digest: string | *""}
	replicas: int | *1
}

debugValues: {
	image: {repository: "nginx", tag: "1.27", digest: ""}
	replicas: 1
}
