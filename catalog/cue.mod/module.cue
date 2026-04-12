module: "opmodel.dev/poc-controller/catalog@v1"
language: {
	version: "v0.15.0"
}
source: {
	kind: "self"
}
deps: {
	"opmodel.dev/opm/v1alpha1@v1": {
		v: "v1.5.6"
	}
	"opmodel.dev/core/v1alpha1@v1": {
		v: "v1.3.2"
	}
	"opmodel.dev/kubernetes/v1@v1": {
		v: "v1.0.1"
	}
	"opmodel.dev/gateway_api/v1alpha1@v1": {
		v: "v1.3.5"
	}
	"opmodel.dev/cert_manager/v1alpha1@v1": {
		v: "v1.3.2"
	}
	"opmodel.dev/k8up/v1alpha1@v1": {
		v: "v1.0.2"
	}
	"cue.dev/x/k8s.io@v0": {
		v:       "v0.7.0"
		default: true
	}
	"cue.dev/x/crd/cert-manager.io@v0": {
		v: "v0.3.0"
	}
}
