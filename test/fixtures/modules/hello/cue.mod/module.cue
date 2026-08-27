module: "testing.opmodel.dev/modules/operator/hello@v0"
language: {
	version: "v0.17.0"
}
source: {
	kind: "self"
}
deps: {
	"opmodel.dev/catalogs/opm@v2": {
		v: "v2.0.0-alpha.6"
	}
	"opmodel.dev/core@v2": {
		v: "v2.0.0-alpha.6"
	}
}
