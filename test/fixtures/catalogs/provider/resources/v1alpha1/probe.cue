package v1alpha1

import (
	id "testing.opmodel.dev/catalogs/operator/provider/identity"
	c "opmodel.dev/core@v2"
)

// #ProbeResource is the one contract this catalog defines: a key-value map
// that its probe transformer renders as a ConfigMap.
#ProbeResource: c.#Resource & {
	metadata: {
		modulePath:     "\(id.kindPrefix.resources)/v1alpha1"
		name:           "probe"
		apiVersion:     "v1alpha1"
		catalogVersion: id.Version
		fqn:            "\(id.kindPrefix.resources)/probe@v1alpha1"
		description:    "A key-value map rendered as one ConfigMap"
	}

	spec: probe: data: [string]: string
}
