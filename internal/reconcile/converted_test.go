package reconcile

import (
	"errors"
	"testing"

	"cuelang.org/go/cue/cuecontext"

	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

func convertTestResource(t *testing.T, src string) *core.Resource {
	t.Helper()
	v := cuecontext.New().CompileString(src)
	if err := v.Err(); err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	return &core.Resource{Value: v}
}

// TestConvertRender_DropsResources checks that convertRender drops the
// rendered CUE values on every exit, so a caller that releases its render
// slot after convertRender never holds a build without a slot.
func TestConvertRender_DropsResources(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		result := &render.RenderResult{Resources: []*core.Resource{
			convertTestResource(t, `{apiVersion: "v1", kind: "ConfigMap", metadata: name: "a"}`),
		}}
		converted, err := convertRender(result)
		if err != nil {
			t.Fatalf("convertRender: %v", err)
		}
		if result.Resources != nil {
			t.Fatal("the rendered resources must be dropped on success")
		}
		if len(converted.resources) != 1 || converted.digest == "" {
			t.Fatalf("want one converted resource and a digest, got %d and %q", len(converted.resources), converted.digest)
		}
	})

	t.Run("export failure", func(t *testing.T) {
		result := &render.RenderResult{Resources: []*core.Resource{
			convertTestResource(t, `{apiVersion: "v1", kind: "ConfigMap", metadata: name: string}`),
		}}
		_, err := convertRender(result)
		var convErr *conversionError
		if !errors.As(err, &convErr) {
			t.Fatalf("want a conversionError for a non-concrete resource, got %v", err)
		}
		if convErr.reason != status.RenderFailedReason {
			t.Fatalf("want reason %q, got %q", status.RenderFailedReason, convErr.reason)
		}
		if result.Resources != nil {
			t.Fatal("the rendered resources must be dropped on a conversion failure too")
		}
	})
}
