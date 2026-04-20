package module

import (
	"context"
	"fmt"

	"cuelang.org/go/cue"

	"github.com/open-platform-model/opm-operator/pkg/validate"
)

// ParseModuleRelease validates values, fills them into the release spec,
// ensures the result is concrete, decodes metadata, and constructs Release.
func ParseModuleRelease(_ context.Context, spec cue.Value, mod Module, values []cue.Value) (*Release, error) {
	// Best-effort name for error messages — metadata.name may already be
	// concrete before values filling (it comes from the module definition).
	name := bestEffortReleaseName(spec, mod)

	// Validate and merge values against the module config schema.
	merged, cfgErr := validate.Config(mod.Config, values, "module", name)
	if cfgErr != nil {
		return nil, cfgErr
	}

	// Fill merged values into the release spec.
	if merged.Exists() {
		spec = spec.FillPath(cue.ParsePath("values"), merged)
		if err := spec.Err(); err != nil {
			return nil, fmt.Errorf("filling values into release spec: %w", err)
		}
	}

	// Validate the filled spec is fully concrete.
	if err := spec.Validate(cue.Concrete(true)); err != nil {
		return nil, fmt.Errorf("release %q: not fully concrete: %w", name, err)
	}

	// Decode release metadata from the concrete spec.
	metadata, err := decodeReleaseMetadata(spec, name)
	if err != nil {
		return nil, err
	}

	return &Release{
		Metadata: metadata,
		Module:   mod,
		Spec:     spec,
		Values:   merged,
	}, nil
}

// decodeReleaseMetadata extracts and decodes ReleaseMetadata from a concrete spec value.
func decodeReleaseMetadata(spec cue.Value, name string) (*ReleaseMetadata, error) {
	metaVal := spec.LookupPath(cue.ParsePath("metadata"))
	if !metaVal.Exists() {
		return nil, fmt.Errorf("release %q: metadata field is required", name)
	}
	meta := &ReleaseMetadata{}
	if err := metaVal.Decode(meta); err != nil {
		return nil, fmt.Errorf("release %q: decoding metadata: %w", name, err)
	}
	return meta, nil
}

// bestEffortReleaseName tries to extract a release name for error messages.
// Falls back to the module name if the release name is not yet available.
func bestEffortReleaseName(spec cue.Value, mod Module) string {
	nameVal := spec.LookupPath(cue.ParsePath("metadata.name"))
	if nameVal.Exists() {
		if s, err := nameVal.String(); err == nil {
			return s
		}
	}
	if mod.Metadata != nil && mod.Metadata.Name != "" {
		return mod.Metadata.Name
	}
	return "<unknown>"
}
