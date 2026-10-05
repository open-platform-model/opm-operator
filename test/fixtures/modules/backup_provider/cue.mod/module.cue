module: "testing.opmodel.dev/modules/operator/backup_provider@v0"
language: {
	version: "v0.17.0"
}
source: {
	kind: "self"
}
deps: {
	"cue.dev/x/k8s.io@v0": {
		v: "v0.12.0"
	}
	"opmodel.dev/catalogs/opm@v4": {
		v: "v4.6.0"
	}
	"opmodel.dev/core@v2": {
		v: "v2.0.0-beta.1"
	}
}
