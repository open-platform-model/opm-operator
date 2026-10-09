package render

import (
	"context"
	"errors"
	"fmt"

	"github.com/open-platform-model/library/opm/k8s/object"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/module"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/moduleacquire"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
)

// ErrPlatformNotReady is returned by the renderers when the platform store
// holds no generated platform module. It is a typed sentinel so the
// reconciler-side mapping to a custom-resource condition can branch on it via
// errors.Is without string matching.
var ErrPlatformNotReady = errors.New("platform not ready: no generated platform module")

// ErrAcquire marks a failure to acquire module source: fetching a module from
// the registry (ModuleInstance) or loading a package and resolving its CUE
// dependencies (ModulePackage). It marks the phase, not the cause, and
// decides only the Ready reason of a stalled failure: an acquisition failure
// reads ResolutionFailed. Whether a failure retries is decided by the
// library's typed registry fetch failure (*oerrors.FetchError, see
// reconcile.IsTransientFailure), in this phase and every later one.
// It never changes the message: errors.Is finds it beside the original error.
var ErrAcquire = errors.New("acquiring module source")

// acquireError marks err as an acquisition failure (ErrAcquire) without
// changing its message; errors.Is finds the sentinel and errors.AsType still
// reaches every typed cause under err.
type acquireError struct {
	msg string
	err error
}

func (e *acquireError) Error() string   { return e.within(conditionMessageLimit) }
func (e *acquireError) Unwrap() []error { return []error{ErrAcquire, e.err} }

// within words the error in at most limit bytes: a cause that can word
// itself within a length does so in what the message leaves; any other text
// is cut.
func (e *acquireError) within(limit int) string {
	head := e.msg + ": "
	if b, ok := e.err.(bounded); ok && limit > len(head) {
		return head + b.within(limit-len(head))
	}
	return cutText(head+e.err.Error(), limit)
}

// acquireFailed wraps err as msg + ": " + err, the same text as
// fmt.Errorf("%s: %w", msg, err), and marks it with ErrAcquire.
func acquireFailed(msg string, err error) error { return &acquireError{msg: msg, err: err} }

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
// defaults apply), synthesizes the instance, renders it against the platform,
// and adapts the compiled output to operator resources plus inventory
// entries. The kernel's synthesis is the one check of the values against the
// module's #config.
//
// Every kernel call shares nothing (library ADR-005, ADR-007): acquisition,
// synthesis and the render build each evaluate in a context of their own, so
// renders of different objects overlap up to the shared render slot count
// (--max-concurrent-renders, across both kinds; the caller holds the slot)
// and no correctness gate applies. The lease is held for the whole call: the
// build reads the platform module directory the record names.
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

	result, err := resultFromRender(out, rec.Identity)
	if err != nil {
		return nil, err
	}
	result.ModuleVersion = declaredModuleVersion(inst)
	result.SkewPolicy = platformstore.SkewPolicyName(rec.Skew)
	return result, nil
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
		return nil, acquireFailed("acquiring module", err)
	}
	return r.synthesizeFrom(ctx, mod, name, namespace, values)
}

// synthesizeFrom synthesizes the instance from the acquired module and the
// values. It is the part of synthesize that needs no registry fetch of the
// module itself. Synthesis checks the values against the module's #config
// (types, constraints, fields the schema does not allow, required values left
// unset); a failure is worded with every finding and its positions
// (withFindings). The positions of the module's own files are left as CUE
// reports them: a module from the registry lies in the CUE module cache,
// whose paths do not change between reconciles.
func (r *KernelModuleRenderer) synthesizeFrom(
	ctx context.Context,
	mod *module.Module,
	name, namespace string,
	values *releasesv1alpha1.RawValues,
) (*module.Instance, error) {
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

	inst, err := r.Kernel.SynthesizeInstance(ctx, kernel.InstanceInput{
		Module:    mod,
		Name:      name,
		Namespace: namespace,
		Values:    []kernel.Source{src},
	})
	if err != nil {
		return nil, withFindings("synthesizing release: ", err, "")
	}
	return inst, nil
}

// resultFromRender adapts the kernel's render output to the operator's
// RenderResult: compiled objects to resources with provenance, the advisory
// rows worded as warnings (renderWarnings), and the rows themselves
// (unhandled traits, resolved versions) carried through for the reconciler.
// It does not export the resources: the reconciler's one export builds the
// render digest, the apply objects and the inventory entries.
//
// The duplicate-identity check runs first, before any resource or warning is
// built: two objects sharing one apply identity reach apply as two writes to
// one object and the last silently overwrites the first, so a refused render
// must leave no resource and no digest for either loop to act on
// (0015:D15). The library's error is
// returned bare so both classifiers find its type and status carries its
// message verbatim.
//
// The instance's contract demand is the kernel's: the render build reports
// it on its diagnostics, computed from the instance alone (0013:D24), and
// both renderers read it here so they cannot fill it differently. An absent
// list becomes an empty one, so status never alternates between the two.
func resultFromRender(
	out *kernel.RenderResult,
	identity platformstore.PackageIdentity,
) (*RenderResult, error) {
	if dups := object.Duplicates(out.Compiled); len(dups) > 0 {
		return nil, &object.DuplicateIdentitiesError{Duplicates: dups}
	}

	return &RenderResult{
		Resources:         object.Resources(out.Compiled),
		Warnings:          renderWarnings(out.Diagnostics),
		UnhandledTraits:   out.Diagnostics.UnhandledTraits,
		ResolvedVersions:  out.Diagnostics.ResolvedVersions,
		RequiredContracts: demandOf(out.Diagnostics),
		PlatformIdentity:  identity.String(),
	}, nil
}

// demandOf returns the contract demand the render reported, normalising an
// absent list to an empty, non-nil one.
func demandOf(diag kernel.RenderDiagnostics) []string {
	if diag.RequiredContracts == nil {
		return []string{}
	}
	return diag.RequiredContracts
}
