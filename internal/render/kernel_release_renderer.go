package render

import (
	"context"
	"errors"
	"fmt"

	loaderfile "github.com/open-platform-model/library/opm/helper/loader/file"
	"github.com/open-platform-model/library/opm/kernel"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/pkg/core"
	"github.com/open-platform-model/opm-operator/pkg/provider"
)

// KernelReleaseRenderer renders a Flux-fetched Release package through the
// library kernel. It is the kernel-backed peer of PackageReleaseRenderer behind
// the ReleaseRenderer seam: for a kind: ModuleRelease package it loads the
// release in the kernel's *cue.Context, reads the materialized platform from the
// store, compiles the release against it, and adapts the compiled output to
// operator resources plus inventory entries. A kind: BundleRelease package is
// rejected with ErrUnsupportedKind, unchanged from the fork.
//
// No values are injected: a Release package is an authored #ModuleRelease that
// already carries its own values, mirroring RenderLoadedModuleRelease's
// nil-values behavior — there is no SynthesizeRelease step.
type KernelReleaseRenderer struct {
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

// KernelReleaseRenderer implements the ReleaseRenderer seam.
var _ ReleaseRenderer = (*KernelReleaseRenderer)(nil)

// Render loads, kind-detects, and renders the release package at packageDir.
//
// Kind detection rides on the loader's shape gate: Kernel.LoadReleasePackage
// gates to the #ModuleRelease kind, so a BundleRelease package fails with
// loaderfile.ErrWrongKind — the library's documented signal for frontends to
// branch on the failure class via errors.Is. That resolves kind detection in
// the kernel's context without a separate non-gated peek or re-coupling to the
// fork loader.
//
// For a ModuleRelease package it builds the release, gates on platform
// readiness (returning ErrPlatformNotReady before Compile when no platform is
// materialized so nothing is applied), compiles against the materialized
// platform, and adapts the output. The legacy *provider.Provider parameter is
// ignored — the platform comes from the injected store.
func (r *KernelReleaseRenderer) Render(
	ctx context.Context,
	packageDir string,
	_ *provider.Provider,
) (string, *RenderResult, error) {
	raw, err := r.Kernel.LoadReleasePackage(ctx, packageDir, loaderfile.LoadOptions{Registry: r.Registry})
	if err != nil {
		if errors.Is(err, loaderfile.ErrWrongKind) {
			// BundleRelease is the only other release kind the controller emits;
			// surface it as unsupported, unchanged from PackageReleaseRenderer.
			return KindBundleRelease, nil, fmt.Errorf("%w: BundleRelease rendering is not yet implemented", ErrUnsupportedKind)
		}
		return KindModuleRelease, nil, fmt.Errorf("loading release package: %w", err)
	}

	rel, err := r.Kernel.NewReleaseFromValue(raw)
	if err != nil {
		return KindModuleRelease, nil, fmt.Errorf("building release: %w", err)
	}

	// Gate on platform readiness ahead of Compile so a release with no
	// materialized platform applies and prunes nothing. Kind is already known,
	// so a BundleRelease is still rejected above even when no platform exists.
	mp, ok := r.Store.Get()
	if !ok {
		return KindModuleRelease, nil, ErrPlatformNotReady
	}

	out, err := r.Kernel.Compile(ctx, kernel.CompileInput{
		ModuleRelease: rel,
		Platform:      mp,
		RuntimeName:   r.RuntimeName,
	})
	if err != nil {
		return KindModuleRelease, nil, fmt.Errorf("compiling module release: %w", err)
	}

	resources := make([]*core.Resource, 0, len(out.Compiled))
	for _, c := range out.Compiled {
		resources = append(resources, core.ResourceFromCompiled(c))
	}

	entries, err := buildInventoryEntries(resources)
	if err != nil {
		return KindModuleRelease, nil, fmt.Errorf("building inventory entries: %w", err)
	}

	return KindModuleRelease, &RenderResult{
		Resources:        resources,
		InventoryEntries: entries,
		Warnings:         out.Warnings,
	}, nil
}
