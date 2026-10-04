// A fixture of the operator module's deployed operator for test-release-check.sh.
package operator

Version: "1.0.0-beta.2"

Image: {
	repository: "ghcr.io/open-platform-model/opm-operator"
	tag:        "v\(Version)"
	digest:     "sha256:1111111111111111111111111111111111111111111111111111111111111111"
}
