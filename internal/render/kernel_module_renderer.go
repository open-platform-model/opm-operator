package render

import (
	"context"
	"errors"
	"fmt"

	"cuelang.org/go/cue"

	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/moduleacquire"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// ErrPlatformNotReady is returned by KernelModuleRenderer.RenderModule when the
// platform store holds no materialized platform. It is a typed sentinel so the
// reconciler-side mapping to a custom-resource condition (a later slice) can
// branch on it via errors.Is without string matching.
var ErrPlatformNotReady = errors.New("platform not ready: no materialized platform")

// KernelModuleRenderer renders a ModuleRelease entirely through the library
// kernel behind the ModuleRenderer seam: it reads the materialized platform
// from the store, acquires the target module from the registry, synthesizes
// the release, and compiles it against the platform.
type KernelModuleRenderer struct {
	// Kernel is the shared, long-lived library Kernel (one per process).
	Kernel *kernel.Kernel

	// Store holds the materialized platform written by the PlatformReconciler.
	Store *platformstore.Store

	// Registry is the CUE_REGISTRY mapping applied per module acquisition.
	Registry string

	// RuntimeName is the runtime identity injected into each transformer's
	// #context (e.g. "opm-controller").
	RuntimeName string
}

// KernelModuleRenderer implements the ModuleRenderer seam.
var _ ModuleRenderer = (*KernelModuleRenderer)(nil)

// RenderModule renders the module at modulePath@moduleVersion into a
// RenderResult via the kernel. It reads the materialized platform from the
// store (returning ErrPlatformNotReady before any I/O when absent), acquires
// the module, compiles supplied values to a cue.Value (the zero value when none
// are supplied, letting the module's #config defaults apply), synthesizes the
// release, compiles it against the platform, and adapts the compiled output to
// operator resources plus inventory entries.
//
// The platform comes from the injected store.
func (r *KernelModuleRenderer) RenderModule(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
) (*RenderResult, error) {
	// Gate before any registry I/O: nothing can be rendered without a platform.
	mp, ok := r.Store.Get()
	if !ok {
		return nil, ErrPlatformNotReady
	}

	mod, err := moduleacquire.Acquire(ctx, r.Kernel, modulePath, moduleVersion, r.Registry)
	if err != nil {
		return nil, fmt.Errorf("acquiring module: %w", err)
	}

	// Convert CRD values to a cue.Value. The zero cue.Value signals "no values
	// supplied" to SynthesizeRelease, which then relies on the module's #config
	// defaults for concreteness — mirroring the legacy path's behavior.
	var cueValues cue.Value
	if values != nil && values.Raw != nil {
		compiled := r.Kernel.CueContext().CompileBytes(values.Raw, cue.Filename("values"))
		if compiled.Err() != nil {
			return nil, fmt.Errorf("compiling values: %w", compiled.Err())
		}
		cueValues = compiled
	}

	rel, err := r.Kernel.SynthesizeRelease(ctx, synth.ReleaseInput{
		Module:    mod,
		Name:      name,
		Namespace: namespace,
		Values:    cueValues,
	})
	if err != nil {
		return nil, fmt.Errorf("synthesizing release: %w", err)
	}

	out, err := r.Kernel.Compile(ctx, kernel.CompileInput{
		ModuleRelease: rel,
		Platform:      mp,
		RuntimeName:   r.RuntimeName,
	})
	if err != nil {
		return nil, fmt.Errorf("compiling module release: %w", err)
	}

	resources := make([]*core.Resource, 0, len(out.Compiled))
	for _, c := range out.Compiled {
		resources = append(resources, core.ResourceFromCompiled(c))
	}

	entries, err := buildInventoryEntries(resources)
	if err != nil {
		return nil, fmt.Errorf("building inventory entries: %w", err)
	}

	return &RenderResult{
		Resources:        resources,
		InventoryEntries: entries,
		Warnings:         out.Warnings,
	}, nil
}
