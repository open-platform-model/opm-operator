// hello-web — minimal container workload. Renders a single Deployment via the
// catalog's deployment-transformer. Pinned to a PRE-FIX catalog (v0.5.2) so it
// exercises the kernel output-local hidden field fix, not the catalog workaround.
package hello_web

import (
	m "opmodel.dev/core@v1"
	res "opmodel.dev/catalogs/opm/resources"
)

m.#Module

metadata: {
	modulePath:  "opmodel.dev/modules/test"
	name:        "hello-web"
	version:     "0.1.2"
	description: "Minimal container workload — renders one Deployment (kernel hidden-field fix probe)"
}

#config: {
	image: res.#Image & {repository: string | *"nginx", tag: string | *"1.27", digest: string | *""}
	replicas: int | *1
}

debugValues: {
	image: {repository: "nginx", tag: "1.27", digest: ""}
	replicas: 1
}
