// Package operator names the one operator release this module version
// deploys. It is a package of its own so a tool reads the deployed operator
// version from the module's source without rendering it:
//
//	cue eval ./operator -e Version
//
// A module release that follows an operator release moves all three values
// together. The version never falls below hack/operator-module/min-operator-version
// (the first operator that refuses to reconcile its own instance); the render
// test enforces that.
package operator

// The operator release this module version deploys, bare SemVer.
Version: "1.0.0-beta.8"

Image: {
	repository: "ghcr.io/open-platform-model/opm-operator"
	tag:        "v\(Version)"
	// The content digest of `tag` on GHCR, read when Version is bumped.
	digest: "sha256:6ce599e344558c960b3f9e0fe5531d9ef30b118830dc8b611563172bf1949f68"
}
