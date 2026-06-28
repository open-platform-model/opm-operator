package render

import (
	"context"
	"errors"
	"fmt"

	loaderfile "github.com/open-platform-model/library/opm/helper/loader/file"
	"github.com/open-platform-model/library/opm/kernel"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// Was: KernelReleaseRenderer
// KernelPackageRenderer renders a Flux-fetched ModulePackage through the
// library kernel behind the PackageRenderer seam: for a kind: ModuleInstance
// package it loads the instance in the kernel's *cue.Context, reads the
// materialized platform from the store, compiles the instance against it, and
// adapts the compiled output to operator resources plus inventory entries. Any
// package whose kind is not #ModuleInstance is rejected with ErrUnsupportedKind.
//
// No values are injected: a ModulePackage references an authored #ModuleInstance
// that already carries its own values — there is no SynthesizeInstance step.
type KernelPackageRenderer struct {
	// Kernel is the shared, long-lived library Kernel (one per process).
	Kernel *kernel.Kernel

	// Store holds the materialized platform written by the PlatformReconciler.
	Store *platformstore.Store

	// Registry is the CUE_REGISTRY mapping applied while loading the package.
	Registry string

	// RuntimeName is the runtime identity injected into each transformer's
	// #context (e.g. "opm-controller").
	RuntimeName string
}

// KernelPackageRenderer implements the PackageRenderer seam.
var _ PackageRenderer = (*KernelPackageRenderer)(nil)

// Render loads, kind-detects, and renders the package at packageDir.
//
// Kind detection rides on the loader's shape gate: Kernel.LoadInstancePackage
// gates to the #ModuleInstance kind, so any other kind fails with
// loaderfile.ErrWrongKind — the library's documented signal for frontends to
// branch on the failure class via errors.Is. That resolves kind detection in
// the kernel's context without a separate non-gated peek or re-coupling to the
// fork loader.
//
// For a ModuleInstance package it builds the instance, gates on platform
// readiness (returning ErrPlatformNotReady before Compile when no platform is
// materialized so nothing is applied), compiles against the materialized
// platform, and adapts the output. The platform comes from the injected store.
func (r *KernelPackageRenderer) Render(
	ctx context.Context,
	packageDir string,
) (string, *RenderResult, error) {
	raw, err := r.Kernel.LoadInstancePackage(ctx, packageDir, loaderfile.LoadOptions{Registry: r.Registry})
	if err != nil {
		if errors.Is(err, loaderfile.ErrWrongKind) {
			// Only #ModuleInstance is renderable; any other kind is unsupported.
			return "", nil, fmt.Errorf("%w: %w", ErrUnsupportedKind, err)
		}
		return KindModuleInstance, nil, fmt.Errorf("loading package: %w", err)
	}

	inst, err := r.Kernel.NewInstanceFromValue(raw)
	if err != nil {
		return KindModuleInstance, nil, fmt.Errorf("building instance: %w", err)
	}

	// Gate on platform readiness ahead of Compile so a package with no
	// materialized platform applies and prunes nothing. Kind is already known,
	// so an unsupported kind is still rejected above even when no platform exists.
	mp, ok := r.Store.Get()
	if !ok {
		return KindModuleInstance, nil, ErrPlatformNotReady
	}

	out, err := r.Kernel.Compile(ctx, kernel.CompileInput{
		ModuleInstance: inst,
		Platform:       mp,
		RuntimeName:    r.RuntimeName,
	})
	if err != nil {
		return KindModuleInstance, nil, fmt.Errorf("compiling module instance: %w", err)
	}

	resources := make([]*core.Resource, 0, len(out.Compiled))
	for _, c := range out.Compiled {
		resources = append(resources, core.ResourceFromCompiled(c))
	}

	entries, err := buildInventoryEntries(resources)
	if err != nil {
		return KindModuleInstance, nil, fmt.Errorf("building inventory entries: %w", err)
	}

	return KindModuleInstance, &RenderResult{
		Resources:        resources,
		InventoryEntries: entries,
		Warnings:         out.Warnings,
	}, nil
}
