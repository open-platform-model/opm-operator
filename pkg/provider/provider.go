// Package provider defines the Provider type.
// A Provider holds a CUE transformer registry. Go performs component-to-
// transformer matching while CUE still executes each matched transformer.
package provider

import (
	"cuelang.org/go/cue"
)

// Provider holds a loaded provider definition.
// Data is the complete CUE value (including #transformers).
// Matching against components happens in Go via pkg/match.
type Provider struct {
	// Metadata is extracted for display and provider selection.
	Metadata *ProviderMetadata

	// Data is the fully evaluated CUE value for the provider,
	// including the transformer registry (#transformers) and all declared resources/traits.
	Data cue.Value
}

// ProviderMetadata holds identity metadata for a provider.
//
//nolint:revive // stutter intentional: provider.ProviderMetadata reads clearly at call sites
type ProviderMetadata struct {
	// Name is the provider name (e.g., "kubernetes").
	Name string `json:"name"`

	// Description is a brief description of the provider.
	Description string `json:"description,omitempty"`

	// Version is the provider version.
	Version string `json:"version"`

	// Labels for provider categorization.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations for additional provider metadata.
	Annotations map[string]string `json:"annotations,omitempty"`
}
