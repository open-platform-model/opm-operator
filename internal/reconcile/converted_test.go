package reconcile

import (
	"errors"
	"testing"

	"cuelang.org/go/cue/cuecontext"

	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
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
// each export step's failure. The conversion is the only export of a
// rendered resource, so these are the first report of a value that will not
// export.
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

// TestConvertRender_EntriesFromTheOneExport checks that the inventory entries
// come from the same export as the apply objects: one entry per object, in
// input order, equal to what the renderer's own export used to build, and
// built after the rendered CUE values are dropped.
func TestConvertRender_EntriesFromTheOneExport(t *testing.T) {
	srcs := []string{
		`{apiVersion: "apps/v1", kind: "Deployment", metadata: {name: "nginx", namespace: "default", labels: "component.opmodel.dev/name": "web"}}`,
		`{apiVersion: "v1", kind: "Namespace", metadata: name: "team"}`,
		`{apiVersion: "v1", kind: "ConfigMap", metadata: {name: "a", namespace: "x"}, data: k: "v"}`,
	}
	build := func() []*object.Resource {
		out := make([]*object.Resource, 0, len(srcs))
		for _, src := range srcs {
			out = append(out, convertTestResource(t, src))
		}
		return out
	}

	result := &render.RenderResult{Resources: build()}
	converted, err := convertRender(result)
	if err != nil {
		t.Fatalf("convertRender: %v", err)
	}
	if result.Resources != nil {
		t.Fatal("the rendered resources must be dropped once converted")
	}
	if len(converted.entries) != len(srcs) {
		t.Fatalf("want %d entries, got %d", len(srcs), len(converted.entries))
	}
	for i, obj := range converted.resources {
		if want := inventory.FromEntry(k8sinventory.NewEntry(obj)); converted.entries[i] != want {
			t.Fatalf("entry %d: got %+v, want %+v", i, converted.entries[i], want)
		}
	}

	// The renderer used to build the entries from a second export of each
	// resource; the one export must yield the same entries.
	for i, r := range build() {
		u, err := r.ToUnstructured()
		if err != nil {
			t.Fatalf("ToUnstructured: %v", err)
		}
		if want := inventory.NewEntryFromResource(u); converted.entries[i] != want {
			t.Fatalf("entry %d differs from the second-export entry: got %+v, want %+v", i, converted.entries[i], want)
		}
	}

	if want := (releasesv1alpha1.InventoryEntry{
		Group: "apps", Kind: "Deployment", Version: "v1", Namespace: "default", Name: "nginx", Component: "web",
	}); converted.entries[0] != want {
		t.Fatalf("deployment entry: got %+v, want %+v", converted.entries[0], want)
	}
}
