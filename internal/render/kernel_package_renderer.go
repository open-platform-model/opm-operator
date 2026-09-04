package render

import (
	"context"
	"errors"
	"fmt"

	loaderfile "github.com/open-platform-model/library/opm/helper/loader/file"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/module"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
)

// Was: KernelReleaseRenderer
// KernelPackageRenderer renders a Flux-fetched ModulePackage through the
// library kernel behind the PackageRenderer seam: for a kind: ModuleInstance
// package it acquires the instance from its directory as a source-carrying
// artifact, leases the generated platform from the store, renders the instance
// against it through the kernel's single-build render, and adapts the compiled
// output to operator resources plus inventory entries. Any package whose kind
// is not #ModuleInstance is rejected with ErrUnsupportedKind.
//
// No values are injected: a ModulePackage references an authored #ModuleInstance
// that already carries its own values — there is no SynthesizeInstance step.
type KernelPackageRenderer struct {
	// Kernel is the shared, long-lived library Kernel (one per process).
	Kernel *kernel.Kernel

	// Store holds the generated platform written by the PlatformReconciler.
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
// Kind detection rides on the loader's shape gate: Kernel.AcquireInstanceFromDir
// gates to the #ModuleInstance kind, so any other kind fails with
// loaderfile.ErrWrongKind — the library's documented signal for frontends to
// branch on the failure class via errors.Is. That resolves kind detection in
// the kernel's context without a separate non-gated peek.
//
// For a ModuleInstance package it acquires the instance under the kernel gate
// (the load evaluates in the shared Kernel's context), gates on platform
// readiness (returning ErrPlatformNotReady before any build when no platform
// module is recorded, so nothing is applied), and renders against the leased
// platform outside the gate; the build shares nothing (library ADR-005).
func (r *KernelPackageRenderer) Render(
	ctx context.Context,
	packageDir string,
) (string, *RenderResult, error) {
	inst, err := r.acquire(ctx, packageDir)
	if err != nil {
		if errors.Is(err, loaderfile.ErrWrongKind) {
			// Only #ModuleInstance is renderable; any other kind is unsupported.
			return "", nil, fmt.Errorf("%w: %w", ErrUnsupportedKind, err)
		}
		return KindModuleInstance, nil, fmt.Errorf("loading package: %w", err)
	}

	// Gate on platform readiness ahead of the build so a package with no
	// generated platform applies and prunes nothing. Kind is already known, so
	// an unsupported kind is still rejected above even when no platform exists.
	rec, releaseLease, ok := r.Store.Lease()
	if !ok {
		return KindModuleInstance, nil, ErrPlatformNotReady
	}
	defer releaseLease()

	out, err := r.Kernel.Render(ctx, kernel.RenderInput{
		Instance:    inst,
		Platform:    rec.Platform,
		RuntimeName: r.RuntimeName,
		Skew:        rec.Skew,
	})
	if err != nil {
		return KindModuleInstance, nil, fmt.Errorf("rendering module instance: %w", err)
	}

	result, err := resultFromRender(out)
	if err != nil {
		return KindModuleInstance, nil, err
	}
	return KindModuleInstance, result, nil
}

// acquire loads the package as a validated, source-carrying instance under
// the kernel gate, released before the caller renders.
func (r *KernelPackageRenderer) acquire(ctx context.Context, packageDir string) (*module.Instance, error) {
	release := r.Store.AcquireKernel()
	defer release()
	return r.Kernel.AcquireInstanceFromDir(ctx, packageDir, loaderfile.LoadOptions{Registry: r.Registry})
}
