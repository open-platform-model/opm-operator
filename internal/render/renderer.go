package render

import (
	"context"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// ModuleRenderer is the injection boundary for module rendering in the
// reconcile loop. Production wires KernelModuleRenderer; tests wire a stub that
// returns a pre-built RenderResult without requiring an OCI registry.
type ModuleRenderer interface {
	RenderModule(
		ctx context.Context,
		name, namespace, modulePath, moduleVersion string,
		values *releasesv1alpha1.RawValues,
		prov *provider.Provider,
	) (*RenderResult, error)
}

// ReleaseRenderer loads a CUE release package from a local directory (already
// extracted from a Flux artifact) and returns its kind plus render output.
// Production wires KernelReleaseRenderer; tests inject a stub.
type ReleaseRenderer interface {
	Render(ctx context.Context, packageDir string, prov *provider.Provider) (kind string, result *RenderResult, err error)
}
