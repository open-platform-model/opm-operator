package module

import (
	"cuelang.org/go/cue"
)

// Release is a fully prepared module release ready for rendering.
// When a *Release exists, all invariants hold: Spec is concrete and complete,
// Values is concrete and merged, Metadata is decoded.
type Release struct {
	// Metadata is the decoded release identity from the concrete release spec.
	Metadata *ReleaseMetadata

	// Module is the original module used to prepare the release.
	Module Module

	// Spec is the concrete, values-filled #ModuleRelease CUE value.
	// Concrete (all regular fields resolved) but NOT finalized — CUE definition
	// fields (#resources, #traits, #blueprints) are preserved. Required by
	// MatchComponents() for component-transformer matching.
	// MUST NOT be passed to FinalizeValue or v.Syntax(cue.Final()).
	Spec cue.Value

	// Values is the concrete, merged values applied to the release.
	Values cue.Value
}

// MatchComponents returns the schema-preserving components value used for
// matching. The returned value keeps definition fields such as #resources,
// #traits, and #blueprints.
func (r *Release) MatchComponents() cue.Value {
	return r.Spec.LookupPath(cue.ParsePath("components"))
}

// ReleaseMetadata contains release-level identity information.
// Used for K8s inventory tracking, resource labeling, and CLI output.
type ReleaseMetadata struct {
	// Name is the release name (from --name or module.metadata.name).
	Name string `json:"name"`

	// Namespace is the target namespace.
	Namespace string `json:"namespace"`

	// UUID is the release identity UUID.
	// Computed by CUE as SHA1(OPMNamespace, moduleUUID:name:namespace).
	UUID string `json:"uuid"`

	// Labels are the merged release labels (module labels + standard opm labels).
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are the merged release annotations.
	Annotations map[string]string `json:"annotations,omitempty"`
}
