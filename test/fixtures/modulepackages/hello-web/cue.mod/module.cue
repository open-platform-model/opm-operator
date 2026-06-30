module: "opmodel.dev/releases/test/hello-web@v0"
language: {
	version: "v0.17.0"
}
source: {
	kind: "self"
}
deps: {
	"opmodel.dev/catalogs/opm@v1": {
		v: "v1.0.0-alpha"
	}
	"opmodel.dev/core@v1": {
		v: "v1.0.0-alpha.1"
	}
	"opmodel.dev/modules/test/hello-web@v0": {
		v: "v0.1.2"
	}
}
