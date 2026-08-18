// redis — stateful example module (opmodel.dev/core@v2). Renders a StatefulSet,
// a headless governing Service (clusterIP: None) for stable per-pod network
// identity, and a PersistentVolumeClaim for /data, with an exec readiness probe
// (`redis-cli ping`). Exercises the catalog's stateful transformer path and the
// exec-probe + headless-Service styles, complementing the stateless podinfo
// example.
package redis

import (
	"strings"

	m "opmodel.dev/core@v2"
	res "opmodel.dev/catalogs/opm/resources/v1beta1"

	id "testing.opmodel.dev/modules/operator/redis/identity"
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
