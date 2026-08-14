// hello_web — minimal container workload (opmodel.dev/core@v2). Renders a
// single Deployment via the catalog's deployment-transformer. Renamed from
// hello-web at the core-v2 crossing: a hyphen violates #SnakeNameType and the
// leaf-equals-name assertion, so name and module path moved together.
package hello_web

import (
	m "opmodel.dev/core@v2"
	res "opmodel.dev/catalogs/opm/resources/v1beta1"
)

m.#Module

metadata: {
	name:        "hello_web"
	modulePath:  "opmodel.dev/modules/test/hello_web@v0"
	version:     "0.1.3"
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
