package reconcile

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

// convertRender exports each rendered resource to JSON once, hashes those
// bytes into the render digest, decodes the same bytes into the unstructured
// copies for apply, and then drops result.Resources on every exit, failures
// included. A rendered resource carries its CUE value, which pins the whole
// build, so the caller runs this inside its render slot: the slot is released
// only once the build can be collected. The memprobe baseline puts the peak
// heap in this export, not in the render itself.
func convertRender(result *render.RenderResult) (*convertedRender, error) {
	defer func() { result.Resources = nil }()
	digest, encoded, err := status.RenderDigestJSON(result.Resources)
	if err != nil {
		return nil, &conversionError{reason: status.RenderFailedReason, step: "computing render digest", err: err}
	}
	resources := make([]*unstructured.Unstructured, 0, len(encoded))
	for i, b := range encoded {
		var obj map[string]any
		if err := json.Unmarshal(b, &obj); err != nil {
			err = fmt.Errorf("converting %s to unstructured: %w", result.Resources[i], err)
			return nil, &conversionError{reason: status.ApplyFailedReason, step: "converting resources", err: err}
		}
		resources = append(resources, &unstructured.Unstructured{Object: obj})
	}
	return &convertedRender{result: result, digest: digest, resources: resources}, nil
}
