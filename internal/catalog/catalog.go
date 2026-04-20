// Package catalog loads the OPM provider from a CUE composition module
// that resolves catalog dependencies from an OCI registry at startup.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/open-platform-model/opm-operator/pkg/loader"
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// LoadProvider loads a provider from a CUE composition module directory.
// It evaluates the composition package, extracts the providers map, and
// delegates to loader.LoadProvider to produce the final provider.
func LoadProvider(catalogDir, providerName string) (*provider.Provider, error) {
	registry, err := loadRegistry(catalogDir)
	if err != nil {
		return nil, fmt.Errorf("loading catalog registry from %s: %w", catalogDir, err)
	}

	p, err := loader.LoadProvider(providerName, registry)
	if err != nil {
		return nil, fmt.Errorf("loading provider %q from catalog: %w", providerName, err)
	}
	return p, nil
}

// loadRegistry evaluates the CUE composition package and extracts the
// providers map into a map[string]cue.Value.
func loadRegistry(catalogDir string) (map[string]cue.Value, error) {
	absDir, err := filepath.Abs(catalogDir)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog directory: %w", err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("accessing catalog directory %q: %w", absDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("catalog path %q is not a directory", absDir)
	}

	cfg := &load.Config{
		Dir: absDir,
	}
	instances := load.Instances([]string{"."}, cfg)
	if len(instances) == 0 {
		return nil, fmt.Errorf("no CUE instances found in %s", absDir)
	}
	if instances[0].Err != nil {
		return nil, fmt.Errorf("loading composition package from %s: %w", absDir, instances[0].Err)
	}

	ctx := cuecontext.New()
	val := ctx.BuildInstance(instances[0])
	if err := val.Err(); err != nil {
		return nil, fmt.Errorf("building composition package from %s: %w", absDir, err)
	}

	providersVal := val.LookupPath(cue.ParsePath("providers"))
	if !providersVal.Exists() {
		return nil, fmt.Errorf("composition package missing providers definition")
	}

	registry := make(map[string]cue.Value)
	iter, err := providersVal.Fields()
	if err != nil {
		return nil, fmt.Errorf("iterating providers fields: %w", err)
	}
	for iter.Next() {
		registry[iter.Selector().String()] = iter.Value()
	}

	if len(registry) == 0 {
		return nil, fmt.Errorf("catalog providers is empty")
	}

	return registry, nil
}
