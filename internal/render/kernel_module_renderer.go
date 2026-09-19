package render

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cueerrors "cuelang.org/go/cue/errors"

	"github.com/open-platform-model/library/opm/helper/objectset"
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

// valuesOrigin is the origin the ModuleInstance's raw values are loaded
// under: the CR field they were read from. A values error is reported at
// this origin (kernel.Source.Origin), so an operator reading the event can
// act on it rather than on an anonymous filename.
const valuesOrigin = "spec.values"

// KernelModuleRenderer renders a ModuleInstance entirely through the library
// kernel behind the ModuleRenderer seam: it leases the generated platform
// record from the store, acquires the target module from the registry,
// synthesizes the instance, and renders it against the platform module through
// the kernel's single-build render (0019:D9).
type KernelModuleRenderer struct {
	// Kernel is the shared, long-lived library Kernel (one per process).
	Kernel *kernel.Kernel

	// Store holds the generated platform written by the PlatformReconciler.
	Store *platformstore.Store

	// Registry is passed through to moduleacquire.Acquire's retained registry
	// parameter and does not affect resolution: the Kernel resolves the
	// mapping it was constructed with (kernel.WithRegistry).
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
// the module, loads the values as one values source with origin spec.values
// (an empty document when none are supplied, letting the module's #config
// defaults apply) and checks it against the module's #config, synthesizes
// the instance, renders it against the platform, and adapts the compiled
// output to operator resources plus inventory entries.
//
// Every kernel call shares nothing (library ADR-005, ADR-007): acquisition,
// synthesis and the render build each evaluate in a context of their own, so
// renders of different objects overlap under --max-concurrent-renders with
// no gate. The lease is held for the whole call: the build reads the
// platform module directory the record names.
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

	contracts, err := declaredContracts(inst)
	if err != nil {
		return nil, fmt.Errorf("reading the instance's contract demand: %w", err)
	}

	return resultFromRender(out, rec.Identity, contracts)
}

// synthesize acquires the module and synthesizes the source-carrying
// instance. The Kernel is safe for concurrent use (library ADR-007), so no
// gate is taken.
func (r *KernelModuleRenderer) synthesize(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
) (*module.Instance, error) {
	mod, err := moduleacquire.Acquire(ctx, r.Kernel, modulePath, moduleVersion, r.Registry)
	if err != nil {
		return nil, fmt.Errorf("acquiring module: %w", err)
	}

	// The CRD values become one values source whose origin names the CR
	// field they came from. A ModuleInstance without spec.values supplies the
	// empty document: the core schema declares the instance's values as an
	// open `_` that synthesis leaves unfilled when the stack is empty, and
	// the concreteness check then refuses it whatever defaults #config
	// carries, so "no values" has to reach synthesis as `{}` for the
	// module's #config defaults to apply.
	raw := []byte("{}")
	if values != nil && values.Raw != nil {
		raw = values.Raw
	}
	src, err := r.Kernel.LoadSourceFromBytes(valuesOrigin, raw)
	if err != nil {
		return nil, fmt.Errorf("compiling values: %w", err)
	}
	sources := []kernel.Source{src}

	// Check the source against the module's #config before synthesis.
	// Synthesis bakes the values into the module's own build, and a
	// violation there surfaces where a component consumed the value (a
	// path inside the module) before the kernel's own per-source check
	// runs; the kernel's layered validation reports it at the source's
	// positions instead, so the error names spec.values.
	if _, err := r.Kernel.ValidateConfigDetailed(mod.ConfigSchema(), sources); err != nil {
		return nil, fmt.Errorf("validating values against the module's #config: %s", cueFindings(err))
	}

	inst, err := r.Kernel.SynthesizeInstance(ctx, kernel.InstanceInput{
		Module:    mod,
		Name:      name,
		Namespace: namespace,
		Values:    sources,
	})
	if err != nil {
		return nil, fmt.Errorf("synthesizing release: %w", err)
	}
	return inst, nil
}

// cueFindings words a CUE error tree as one finding per entry, each followed
// by the positions CUE attributed it to, so a values violation reads
// `message: conflicting values ... (spec.values:1:13, ...)`. A non-CUE error
// is returned as its own message.
func cueFindings(err error) string {
	findings := cueerrors.Errors(err)
	lines := make([]string, 0, len(findings))
	for _, e := range findings {
		line := e.Error()
		if positions := cueerrors.Positions(e); len(positions) > 0 {
			at := make([]string, 0, len(positions))
			for _, p := range positions {
				at = append(at, p.String())
			}
			line += " (" + strings.Join(at, ", ") + ")"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return err.Error()
	}
	return strings.Join(lines, "; ")
}

// resultFromRender adapts the kernel's render output to the operator's
// RenderResult: compiled objects to resources with provenance, inventory
// entries built through the existing ToUnstructured bridge, the advisory
// rows worded as warnings (renderWarnings), and the rows themselves
// (unhandled traits, resolved versions) carried through for the reconciler.
//
// The duplicate-identity check runs first, before any resource, inventory
// entry or warning is built: two objects sharing one apply identity reach
// apply as two writes to one object and the last silently overwrites the
// first, so a refused render must leave no resource, no inventory entry and
// no digest for either loop to act on (0015:D15). The library's error is
// returned bare so both classifiers find its type and status carries its
// message verbatim.
//
// contracts is the instance's declared demand, computed by the caller from
// the instance rather than from this output: the kernel reports matched
// PAIRS, which name transformers, and turning a transformer back into the
// contracts it serves would need the platform.
func resultFromRender(
	out *kernel.RenderResult,
	identity platformstore.PackageIdentity,
	contracts []string,
) (*RenderResult, error) {
	if dups := objectset.Duplicates(out.Compiled); len(dups) > 0 {
		return nil, &objectset.DuplicateIdentitiesError{Duplicates: dups}
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
		Resources:         resources,
		InventoryEntries:  entries,
		Warnings:          renderWarnings(out.Diagnostics),
		UnhandledTraits:   out.Diagnostics.UnhandledTraits,
		ResolvedVersions:  out.Diagnostics.ResolvedVersions,
		RequiredContracts: contracts,
		PlatformIdentity:  identity.String(),
	}, nil
}
