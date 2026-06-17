package podinfo

import (
	bp "opmodel.dev/catalogs/opm/blueprints/workload"
	tr "opmodel.dev/catalogs/opm/traits"
)

#components: {
	podinfo: {
		// StatelessWorkload gates the deployment-transformer
		// (requiredLabels: workload-type=stateless); the Expose trait gates the
		// service-transformer so a ClusterIP Service is rendered alongside the
		// Deployment.
		bp.#StatelessWorkload
		tr.#Expose

		metadata: {
			name: "podinfo"
			labels: "core.opmodel.dev/workload-type": "stateless"
		}

		spec: {
			statelessWorkload: {
				container: {
					name:  "podinfo"
					image: #config.image
					ports: http: {name: "http", targetPort: 9898}

					// HTTP health probes against podinfo's built-in endpoints.
					livenessProbe: httpGet: {path: "/healthz", port: 9898}
					readinessProbe: httpGet: {path: "/readyz", port: 9898}
				}
				scaling: count: #config.replicas
				restartPolicy: "Always"
				updateStrategy: type: "RollingUpdate"
			}

			// Service exposing the HTTP port (9898).
			expose: {
				type: "ClusterIP"
				ports: http: {name: "http", targetPort: 9898}
			}
		}
	}
}
