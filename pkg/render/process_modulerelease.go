package render

import (
	"context"
	"fmt"

	"github.com/open-platform-model/poc-controller/pkg/module"
	"github.com/open-platform-model/poc-controller/pkg/provider"
)

// ProcessModuleRelease renders a prepared release with the given provider.
// The release must already be fully prepared via module.ParseModuleRelease.
// If runtimeLabels is non-nil, it overrides the default runtime labels injected
// into each transformer's #context.#runtimeLabels during execution.
func ProcessModuleRelease(
	ctx context.Context,
	rel *module.Release,
	p *provider.Provider,
	runtimeLabels map[string]string,
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
	renderer.runtimeLabels = runtimeLabels
	return renderer.Execute(ctx, rel, schemaComponents, dataComponents, plan)
}
