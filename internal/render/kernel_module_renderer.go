package render

import (
	"context"
	"errors"
	"fmt"

	"cuelang.org/go/cue"

	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/module"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/moduleacquire"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// ErrPlatformNotReady is returned by the renderers when the platform store
// holds no generated platform module. It is a typed sentinel so the
// reconciler-side mapping to a custom-resource condition can branch on it via
// errors.Is without string matching.
var ErrPlatformNotReady = errors.New("platform not ready: no generated platform module")

// KernelModuleRenderer renders a ModuleInstance entirely through the library
// kernel behind the ModuleRenderer seam: it leases the generated platform
// record from the store, acquires the target module from the registry,
// synthesizes the instance, and renders it against the platform module through
// the kernel's single-build render (enhancement 0019 D9).
type KernelModuleRenderer struct {
	// Kernel is the shared, long-lived library Kernel (one per process).
	Kernel *kernel.Kernel

	// Store holds the generated platform written by the PlatformReconciler.
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
// RenderResult via the kernel. It leases the generated platform from the
// store (returning ErrPlatformNotReady before any I/O when absent), acquires
// the module, compiles supplied values to a cue.Value (the zero value when none
// are supplied, letting the module's #config defaults apply), synthesizes the
// instance, renders it against the platform, and adapts the compiled output to
// operator resources plus inventory entries.
//
// The kernel gate is held only across acquisition and synthesis, the calls
// that evaluate in the shared Kernel's own context; the render build shares
// nothing (library ADR-005) and runs outside the gate, so renders of different
// objects overlap under --max-concurrent-renders. The lease is held for the
// whole call: the build reads the platform module directory the record names.
func (r *KernelModuleRenderer) RenderModule(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
) (*RenderResult, error) {
	// Gate before any registry I/O: nothing can be rendered without a platform.
	rec, releaseLease, ok := r.Store.Lease()
	if !ok {
		return nil, ErrPlatformNotReady
	}
	defer releaseLease()

	inst, err := r.synthesize(ctx, name, namespace, modulePath, moduleVersion, values)
	if err != nil {
		return nil, err
	}

	out, err := r.Kernel.Render(ctx, kernel.RenderInput{
		Instance:    inst,
		Platform:    rec.Platform,
		RuntimeName: r.RuntimeName,
		Skew:        rec.Skew,
	})
	if err != nil {
		return nil, fmt.Errorf("rendering module instance: %w", err)
	}

	return resultFromRender(out)
}

// synthesize acquires the module and synthesizes the source-carrying instance
// under the kernel gate, released before the caller renders.
func (r *KernelModuleRenderer) synthesize(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
) (*module.Instance, error) {
	release := r.Store.AcquireKernel()
	defer release()

	mod, err := moduleacquire.Acquire(ctx, r.Kernel, modulePath, moduleVersion, r.Registry)
	if err != nil {
		return nil, fmt.Errorf("acquiring module: %w", err)
	}

	// Convert CRD values to a cue.Value. The zero cue.Value signals "no values
	// supplied" to SynthesizeInstance, which then relies on the module's #config
	// defaults for concreteness.
	var cueValues cue.Value
	if values != nil && values.Raw != nil {
		compiled := r.Kernel.CueContext().CompileBytes(values.Raw, cue.Filename("values"))
		if compiled.Err() != nil {
			return nil, fmt.Errorf("compiling values: %w", compiled.Err())
		}
		cueValues = compiled
	}

	inst, err := r.Kernel.SynthesizeInstance(ctx, synth.InstanceInput{
		Module:    mod,
		Name:      name,
		Namespace: namespace,
		Values:    cueValues,
	})
	if err != nil {
		return nil, fmt.Errorf("synthesizing release: %w", err)
	}
	return inst, nil
}

// resultFromRender adapts the kernel's render output to the operator's
// RenderResult: compiled objects to resources with provenance, inventory
// entries built through the existing ToUnstructured bridge, and the build's
// warnings and resolved-version rows carried through for the reconciler.
func resultFromRender(out *kernel.RenderResult) (*RenderResult, error) {
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
		ResolvedVersions: out.Diagnostics.ResolvedVersions,
	}, nil
}
