package redis

import (
	bp "opmodel.dev/catalogs/opm/blueprints/workload"
	tr "opmodel.dev/catalogs/opm/traits"
)

#components: {
	redis: {
		// StatefulWorkload gates the statefulset-transformer
		// (requiredLabels: workload-type=stateful) and adds the Volumes resource
		// (→ PVC). The Expose trait with clusterIP: "None" gates the
		// service-transformer to render a headless governing Service.
		bp.#StatefulWorkload
		tr.#Expose

		metadata: {
			name: "redis"
			labels: "core.opmodel.dev/workload-type": "stateful"
		}

		// "data" volume source: durable PVC by default, ephemeral emptyDir when
		// persistence is disabled (see #config.persistence). The catalog's
		// volumeMount schema embeds the volume source, so the same source is
		// unified into both the container mount and the component volumes map
		// (the transformer reads the source from spec.volumes and only name +
		// mountPath from the mount).
		let _dataVolume = {
			name:     "data"
			readOnly: false
			if #config.persistence.enabled {
				persistentClaim: {
					size:         #config.persistence.size
					accessMode:   "ReadWriteOnce"
					storageClass: #config.persistence.storageClass
				}
			}
			if !#config.persistence.enabled {
				emptyDir: {}
			}
		}

		spec: {
			statefulWorkload: {
				container: {
					name:  "redis"
					image: #config.image
					ports: redis: {name: "redis", targetPort: 6379}

					// Exec readiness probe — redis is Ready once it answers PING.
					readinessProbe: exec: command: ["redis-cli", "ping"]

					volumeMounts: data: _dataVolume & {mountPath: "/data"}
				}

				volumes: data: _dataVolume

				scaling: count: 1
				restartPolicy: "Always"
				updateStrategy: type: "RollingUpdate"
			}

			// Headless governing Service for the StatefulSet.
			expose: {
				type:      "ClusterIP"
				clusterIP: "None"
				ports: redis: {name: "redis", targetPort: 6379}
			}
		}
	}
}
