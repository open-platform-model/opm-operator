package render

import (
	"context"
	"fmt"

	"github.com/open-platform-model/opm-operator/pkg/module"
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// ProcessModuleRelease renders a prepared release with the given provider.
// The release must already be fully prepared via module.ParseModuleRelease.
// If runtimeName is non-empty, it overrides the default runtime identity
// injected into each transformer's #context.#runtimeName during execution.
func ProcessModuleRelease(
	ctx context.Context,
	rel *module.Release,
	p *provider.Provider,
	runtimeName string,
) (*ModuleResult, error) {
	schemaComponents := rel.MatchComponents()
	if !schemaComponents.Exists() {
		return nil, fmt.Errorf("release %q: no components field in release spec", rel.Metadata.Name)
	}

	dataComponents, err := FinalizeValue(p.Data.Context(), schemaComponents)
	if err != nil {
		return nil, fmt.Errorf("finalizing components: %w", err)
	}

	plan, err := Match(schemaComponents, p)
	if err != nil {
		return nil, err
	}

	renderer := NewModule(p)
	renderer.runtimeName = runtimeName
	return renderer.Execute(ctx, rel, schemaComponents, dataComponents, plan)
}
