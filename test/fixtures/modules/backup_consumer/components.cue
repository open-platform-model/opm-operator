package backup_consumer

import (
	res "opmodel.dev/catalogs/opm/resources/v1beta1"
	tra "opmodel.dev/catalogs/opm/traits/v1alpha1"
)

#components: {
	// WHY a volume and no workload: the backup trait applies only to a
	// component carrying volumes, and an emptyDir renders nothing of its own
	// (opm's PVC transformer emits a claim only for persistentClaim), so the
	// one object this module renders is the backup catalog's
	// ConfigMap. The instance therefore reaches Ready on any cluster, with no
	// storage class and no image pull.
	data: {
		res.#Volumes
		tra.#Backup

		metadata: name: "data"

		spec: {
			volumes: data: {
				emptyDir: {}
				readOnly: false
			}
			backup: {
				schedule: #config.schedule
				retention: keepDaily: #config.keepDaily
			}
		}
	}
}
