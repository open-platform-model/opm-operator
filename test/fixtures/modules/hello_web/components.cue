package hello_web

import bp "opmodel.dev/catalogs/opm/blueprints/v1beta1"

#components: {
	web: {
		metadata: name: "web"
		// StatelessWorkload stamps the workload-type=stateless label that
		// selects the deployment-transformer.
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
