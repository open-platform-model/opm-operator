package render

import (
	"context"
	"fmt"

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

// ReleaseRenderer loads a CUE release package from a local directory (already
// extracted from a Flux artifact) and returns its kind plus render output.
// Production wires PackageReleaseRenderer; tests inject a stub.
type ReleaseRenderer interface {
	Render(ctx context.Context, packageDir string, prov *provider.Provider) (kind string, result *RenderResult, err error)
}

// PackageReleaseRenderer is the production ReleaseRenderer. It evaluates the
// CUE package, detects kind, and dispatches to the ModuleRelease pipeline.
// BundleRelease returns ErrUnsupportedKind.
type PackageReleaseRenderer struct{}

// Render loads, detects kind, and renders a release package.
func (PackageReleaseRenderer) Render(
	ctx context.Context,
	packageDir string,
	prov *provider.Provider,
) (string, *RenderResult, error) {
	raw, kind, err := LoadReleaseFromPath(packageDir)
	if err != nil {
		return "", nil, err
	}
	switch kind {
	case KindModuleRelease:
		result, err := RenderLoadedModuleRelease(ctx, raw, packageDir, prov)
		return kind, result, err
	case KindBundleRelease:
		return kind, nil, fmt.Errorf("%w: BundleRelease rendering is not yet implemented", ErrUnsupportedKind)
	default:
		return kind, nil, fmt.Errorf("%w: %q", ErrUnsupportedKind, kind)
	}
}
