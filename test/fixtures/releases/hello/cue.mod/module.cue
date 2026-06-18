module: "opmodel.dev/releases/test/hello@v0"
language: {
	version: "v0.16.1"
}
source: {
	kind: "self"
}
deps: {
	"opmodel.dev/catalogs/opm@v0": {
		v: "v0.6.0"
	}
	"opmodel.dev/core@v0": {
		v: "v0.5.0"
	}
	"opmodel.dev/modules/test/hello@v0": {
		v: "v0.0.2"
	}
}
