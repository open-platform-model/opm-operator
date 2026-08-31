module: "testing.opmodel.dev/modules/operator/hello_web@v0"
language: {
	version: "v0.17.0"
}
source: {
	kind: "self"
}
deps: {
	"opmodel.dev/catalogs/opm@v4": {
		v: "v4.0.1"
	}
	"opmodel.dev/core@v2": {
		v: "v2.0.0-alpha.6"
	}
}
