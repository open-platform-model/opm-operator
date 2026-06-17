// redis — stateful example module (opmodel.dev/core@v0). Renders a StatefulSet,
// a headless governing Service (clusterIP: None) for stable per-pod network
// identity, and a PersistentVolumeClaim for /data, with an exec readiness probe
// (`redis-cli ping`). Exercises the catalog's stateful transformer path and the
// exec-probe + headless-Service styles, complementing the stateless podinfo
// example.
package redis

import (
	m "opmodel.dev/core@v0"
	res "opmodel.dev/catalogs/opm/resources"
)

m.#Module

metadata: {
	modulePath:  "opmodel.dev/modules/test"
	name:        "redis"
	version:     "0.1.0"
	description: "Stateful example — StatefulSet + headless Service + PVC with a redis-cli exec readiness probe"
}

#config: {
	// Container image. Defaults to upstream redis; override via ModuleRelease values.
	image: res.#Image & {repository: string | *"redis", tag: string | *"7.4", digest: string | *""}

	// Persistence. The DEFAULT is a durable PersistentVolumeClaim (survives pod
	// restarts/rescheduling). Set persistence.enabled: false to fall back to an
	// ephemeral emptyDir instead — data is lost when the pod restarts, which is
	// only appropriate for throwaway demos. Both modes are overridable via the
	// ModuleRelease values.
	persistence: {
		enabled:      bool | *true
		size:         string | *"1Gi"
		storageClass: string | *"standard"
	}
}

debugValues: {
	image: {repository: "redis", tag: "7.4", digest: ""}
	persistence: {enabled: true, size: "1Gi", storageClass: "standard"}
}
