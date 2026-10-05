package reconcile

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/open-platform-model/library/opm/k8s/object"

	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// convertedRender is a render with its CUE values exported: the render
// digest, the unstructured copies every later phase applies, and the result
// with its Resources dropped (the plain data on it stays readable).
type convertedRender struct {
	result    *render.RenderResult
	digest    string
	resources []*unstructured.Unstructured
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
// library's object.Export, hashes those bytes into the render digest, takes
// the objects decoded from the same bytes as the unstructured copies for
// apply, and then drops result.Resources on every exit, failures included. A
// rendered resource carries its CUE value, which pins the whole build, so the
// caller runs this inside its render slot: the slot is released only once the
// build can be collected. The memprobe baseline puts the peak heap in this
// export, not in the render itself.
//
// A failure keeps the reason and message it had before the export moved to
// the library: a value that will not export is a render failure, exported
// JSON that will not decode to an object is an apply failure.
func convertRender(result *render.RenderResult) (*convertedRender, error) {
	defer func() { result.Resources = nil }()
	exported, err := object.Export(result.Resources)
	if err != nil {
		return nil, exportFailure(err)
	}
	resources := make([]*unstructured.Unstructured, len(exported))
	for i := range exported {
		resources[i] = exported[i].Object
	}
	return &convertedRender{result: result, digest: status.RenderDigest(exported), resources: resources}, nil
}

// exportFailure maps an object.Export failure to the conversion error the
// reconciler reported for the same step before.
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
