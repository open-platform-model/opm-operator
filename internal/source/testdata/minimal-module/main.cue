package minimal

output: {
	kind:       "ConfigMap"
	apiVersion: "v1"
	metadata: {
		name:      "cue-oci-minimal"
		namespace: "default"
	}
	data: {
		message: "hello from native cue oci"
	}
}
