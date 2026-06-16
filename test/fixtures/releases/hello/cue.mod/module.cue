module: "testing.opmodel.dev/releases/hello@v0"
language: {
	version: "v0.16.1"
}
source: {
	kind: "self"
}
deps: {
	"cue.dev/x/k8s.io@v0": {
		v: "v0.7.0"
	}
	"opmodel.dev/catalogs/opm@v0": {
		v: "v0.5.1"
	}
	"opmodel.dev/core@v0": {
		v: "v0.5.0"
	}
	"testing.opmodel.dev/modules/hello@v0": {
		v: "v0.0.2"
	}
}
