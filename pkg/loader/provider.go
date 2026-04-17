// Package loader provides functions to load providers and module releases
// from CUE module directories. These are the entry points for embedding
// the OPM engine in other tools.
package loader

import (
	"fmt"
	"sort"

	"cuelang.org/go/cue"

	"github.com/open-platform-model/poc-controller/pkg/provider"
)

// LoadProvider selects and wraps a provider from the pre-loaded config providers map.
//
// providers is the map of provider CUE values loaded from config (GlobalConfig.Providers).
// providerName selects which provider to use. If empty, defaults to "kubernetes".
// If the named provider is not found, an error listing available names is returned.
func LoadProvider(providerName string, providers map[string]cue.Value) (*provider.Provider, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers configured — add a providers block to config.cue")
	}

	if providerName == "" {
		providerName = "kubernetes"
	}

	providerVal, ok := providers[providerName]
	if !ok {
		available := sortedKeys(providers)
		return nil, fmt.Errorf("provider %q not found (available: %v)", providerName, available)
	}

	meta, err := extractProviderMetadata(providerVal, providerName)
	if err != nil {
		return nil, fmt.Errorf("extracting provider metadata for %q: %w", providerName, err)
	}

	return &provider.Provider{
		Metadata: meta,
		Data:     providerVal,
	}, nil
}

// extractProviderMetadata decodes the provider metadata struct directly using
// Decode(), falling back to configKeyName for metadata.name when the field is absent.
func extractProviderMetadata(v cue.Value, configKeyName string) (*provider.ProviderMetadata, error) {
	meta := &provider.ProviderMetadata{Name: configKeyName}

	metaVal := v.LookupPath(cue.ParsePath("metadata"))
	if !metaVal.Exists() {
		// Provider has no metadata block — use config key name as the name.
		return meta, nil
	}
	if err := metaVal.Decode(meta); err != nil {
		return nil, fmt.Errorf("decoding provider metadata: %w", err)
	}
	// Preserve the fallback: if CUE metadata.name decoded as empty, use configKeyName.
	if meta.Name == "" {
		meta.Name = configKeyName
	}
	return meta, nil
}

// sortedKeys returns the sorted keys of a map[string]cue.Value.
func sortedKeys(m map[string]cue.Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
