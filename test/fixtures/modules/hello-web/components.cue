package hello_web

import bp "opmodel.dev/catalogs/opm/blueprints/workload"

#components: {
	web: {
		metadata: {
			name: "web"
			// Gates the deployment-transformer (requiredLabels: workload-type=stateless).
			labels: "core.opmodel.dev/workload-type": "stateless"
		}
		bp.#StatelessWorkload
		spec: statelessWorkload: {
			container: {
				name:  "web"
				image: #config.image
				ports: http: {name: "http", targetPort: 8080}
			}
			scaling: count: #config.replicas
			restartPolicy: "Always"
			updateStrategy: type: "RollingUpdate"
		}
	}
}
