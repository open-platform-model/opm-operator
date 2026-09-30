module: "testing.opmodel.dev/releases/operator/hello_web@v0"
language: {
	version: "v0.17.0"
}
source: {
	kind: "self"
}
deps: {
	"opmodel.dev/catalogs/opm@v4": {
		v: "v4.4.4"
	}
	"opmodel.dev/core@v2": {
		v: "v2.0.0-beta.1"
	}
	"testing.opmodel.dev/modules/operator/hello_web@v0": {
		v: "v0.1.10"
	}
}
