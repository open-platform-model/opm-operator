package render

import (
	"context"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// ModuleRenderer is the injection boundary for module rendering in the
// reconcile loop. Production wires KernelModuleRenderer; tests wire a stub that
// returns a pre-built RenderResult without requiring an OCI registry.
type ModuleRenderer interface {
	RenderModule(
		ctx context.Context,
		name, namespace, modulePath, moduleVersion string,
		values *releasesv1alpha1.RawValues,
	) (*RenderResult, error)
}

// Was: ReleaseRenderer
// PackageRenderer loads a CUE package from a local directory (already
// extracted from a Flux artifact) and returns its kind plus render output.
// Production wires KernelPackageRenderer; tests inject a stub.
type PackageRenderer interface {
	Render(ctx context.Context, packageDir string) (kind string, result *RenderResult, err error)
}
