package catalog

import (
	provs "opmodel.dev/opm/v1alpha1/providers@v1"
	k8s_provs "opmodel.dev/kubernetes/v1/providers/kubernetes@v1"
	gw_provs "opmodel.dev/gateway_api/v1alpha1/providers/kubernetes@v1"
	cm_provs "opmodel.dev/cert_manager/v1alpha1/providers/kubernetes@v1"
	k8up_provs "opmodel.dev/k8up/v1alpha1/providers/kubernetes@v1"
)

providers: {
	kubernetes: provs.#Registry["kubernetes"] & {
		#transformers: k8s_provs.#Provider.#transformers
		#transformers: gw_provs.#Provider.#transformers
		#transformers: cm_provs.#Provider.#transformers
		#transformers: k8up_provs.#Provider.#transformers
	}
}
