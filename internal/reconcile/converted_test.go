package reconcile

import (
	"errors"
	"testing"

	"cuelang.org/go/cue/cuecontext"

	"github.com/open-platform-model/library/opm/k8s/object"

	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

func convertTestResource(t *testing.T, src string) *object.Resource {
	t.Helper()
	v := cuecontext.New().CompileString(src)
	if err := v.Err(); err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	return &object.Resource{Value: v}
}

// TestConvertRender_DropsResources checks that convertRender drops the
// rendered CUE values on every exit, so a caller that releases its render
// slot after convertRender never holds a build without a slot.
func TestConvertRender_DropsResources(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		result := &render.RenderResult{Resources: []*object.Resource{
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
		result := &render.RenderResult{Resources: []*object.Resource{
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

// TestConvertRender_OneExport checks that the digest and the apply objects
// come from one export: the digest is RenderDigest over object.Export of the
// same resources, and the apply objects are that export's objects in input
// order.
func TestConvertRender_OneExport(t *testing.T) {
	srcs := []string{
		`{apiVersion: "v1", kind: "Service", metadata: {name: "b", namespace: "x"}}`,
		`{apiVersion: "v1", kind: "ConfigMap", metadata: {name: "a", namespace: "x"}, data: k: "v"}`,
	}
	build := func() []*object.Resource {
		out := make([]*object.Resource, 0, len(srcs))
		for _, src := range srcs {
			out = append(out, convertTestResource(t, src))
		}
		return out
	}
	want, err := object.Export(build())
	if err != nil {
		t.Fatalf("object.Export: %v", err)
	}

	converted, err := convertRender(&render.RenderResult{Resources: build()})
	if err != nil {
		t.Fatalf("convertRender: %v", err)
	}
	if got, wantDigest := converted.digest, status.RenderDigest(want); got != wantDigest {
		t.Fatalf("digest: got %q, want %q", got, wantDigest)
	}
	if len(converted.resources) != len(want) {
		t.Fatalf("want %d apply objects, got %d", len(want), len(converted.resources))
	}
	for i := range want {
		if got, w := converted.resources[i].GetName(), want[i].Object.GetName(); got != w {
			t.Fatalf("apply object %d: got %q, want %q (input order)", i, got, w)
		}
	}
}

// TestConvertRender_FailureMessages pins the reason and the exact message of
// each export step's failure, unchanged from before the export moved to the
// library.
func TestConvertRender_FailureMessages(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		src := `{apiVersion: "v1", kind: "ConfigMap", metadata: name: string}`
		_, cause := convertTestResource(t, src).MarshalJSON()
		if cause == nil {
			t.Fatal("want the non-concrete resource to fail its export")
		}
		_, err := convertRender(&render.RenderResult{Resources: []*object.Resource{convertTestResource(t, src)}})
		var convErr *conversionError
		if !errors.As(err, &convErr) {
			t.Fatalf("want a conversionError, got %v", err)
		}
		if convErr.reason != status.RenderFailedReason {
			t.Fatalf("want reason %q, got %q", status.RenderFailedReason, convErr.reason)
		}
		if want := "computing render digest: render digest: " + cause.Error(); err.Error() != want {
			t.Fatalf("message:\n got %q\nwant %q", err.Error(), want)
		}
	})

	t.Run("decode", func(t *testing.T) {
		r := convertTestResource(t, `[{kind: "ConfigMap"}]`)
		_, err := convertRender(&render.RenderResult{Resources: []*object.Resource{r}})
		var convErr *conversionError
		if !errors.As(err, &convErr) {
			t.Fatalf("want a conversionError, got %v", err)
		}
		if convErr.reason != status.ApplyFailedReason {
			t.Fatalf("want reason %q, got %q", status.ApplyFailedReason, convErr.reason)
		}
		want := "converting resources: converting " + r.String() +
			" to unstructured: json: cannot unmarshal array into Go value of type map[string]interface {}"
		if err.Error() != want {
			t.Fatalf("message:\n got %q\nwant %q", err.Error(), want)
		}
	})
}
