package podinfo

import (
	bp "opmodel.dev/catalogs/opm/blueprints/v1beta1"
	tr "opmodel.dev/catalogs/opm/traits/v1beta1"
)

#components: {
	podinfo: {
		// StatelessWorkload stamps the workload-type=stateless label that
		// selects the deployment-transformer; the Expose trait gates the
		// service-transformer so a ClusterIP Service is rendered alongside the
		// Deployment.
		bp.#StatelessWorkload
		tr.#Expose

		metadata: name: "podinfo"

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
