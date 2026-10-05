package reconcile

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// convertedRender is a render with its CUE values exported: the render
// digest, the unstructured copies every later phase applies, the inventory
// entries of the full rendered set, and the result with its Resources
// dropped (the plain data on it stays readable).
type convertedRender struct {
	result    *render.RenderResult
	digest    string
	resources []*unstructured.Unstructured
	// entries holds one inventory entry per exported object, in export
	// order, built before any resource is withheld from the apply list, so
	// a withheld resource stays in the inventory.
	entries []releasesv1alpha1.InventoryEntry
}

// conversionError is a failure to export a rendered set. reason is the
// condition reason the caller marks the object Stalled with.
type conversionError struct {
	reason string
	step   string
	err    error
}

func (e *conversionError) Error() string { return e.step + ": " + e.err.Error() }
func (e *conversionError) Unwrap() error { return e.err }

// convertRender exports each rendered resource from CUE once through the
// library's object.Export and drops result.Resources as soon as the export
// returns (and on every failure exit). A rendered resource carries its CUE
// value, which pins the whole build, so the caller runs this inside its
// render slot: the slot is released only once the build can be collected.
// The memprobe baseline puts the peak heap in this export, not in the render
// itself. Everything else reads exported data only: the render digest hashes
// the exported bytes, the unstructured copies for apply are the objects
// decoded from them, and the inventory entries are built from those objects.
// This is the only export of a rendered resource.
//
// A failure is reported by this conversion, which is the first place an
// unexportable value surfaces: a value that will not export is a render
// failure, exported JSON that will not decode to an object is an apply
// failure.
func convertRender(result *render.RenderResult) (*convertedRender, error) {
	defer func() { result.Resources = nil }()
	exported, err := object.Export(result.Resources)
	result.Resources = nil
	if err != nil {
		return nil, exportFailure(err)
	}
	resources := make([]*unstructured.Unstructured, len(exported))
	entries := make([]releasesv1alpha1.InventoryEntry, len(exported))
	for i := range exported {
		resources[i] = exported[i].Object
		entries[i] = inventory.FromEntry(k8sinventory.NewEntry(exported[i].Object))
	}
	return &convertedRender{
		result:    result,
		digest:    status.RenderDigest(exported),
		resources: resources,
		entries:   entries,
	}, nil
}

// exportFailure maps an object.Export failure to its conversion error.
func exportFailure(err error) *conversionError {
	exportErr, ok := errors.AsType[*object.ExportError](err)
	if !ok {
		return &conversionError{reason: status.RenderFailedReason, step: "computing render digest", err: err}
	}
	if exportErr.Step == object.ExportDecode {
		err = fmt.Errorf("converting %s to unstructured: %w", exportErr.Resource, exportErr.Err)
		return &conversionError{reason: status.ApplyFailedReason, step: "converting resources", err: err}
	}
	return &conversionError{
		reason: status.RenderFailedReason,
		step:   "computing render digest",
		err:    fmt.Errorf("render digest: %w", exportErr.Err),
	}
}
