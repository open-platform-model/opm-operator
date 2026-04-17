package render

import (
	"context"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/pkg/provider"
)

// ModuleRenderer is the injection boundary for module rendering in the
// reconcile loop. Production wires RegistryRenderer; tests wire a stub that
// returns a pre-built RenderResult without requiring an OCI registry.
type ModuleRenderer interface {
	RenderModule(
		ctx context.Context,
		name, namespace, modulePath, moduleVersion string,
		values *releasesv1alpha1.RawValues,
		prov *provider.Provider,
	) (*RenderResult, error)
}

// RegistryRenderer is the production implementation that resolves and renders
// modules from an OCI registry via RenderModuleFromRegistry.
type RegistryRenderer struct{}

// RenderModule delegates to RenderModuleFromRegistry.
func (r *RegistryRenderer) RenderModule(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
	prov *provider.Provider,
) (*RenderResult, error) {
	return RenderModuleFromRegistry(ctx, name, namespace, modulePath, moduleVersion, values, prov)
}
