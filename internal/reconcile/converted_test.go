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
// come from one export: the digest is the library's RenderDigest over
// object.Export of the same resources, and the apply objects are that export's objects in input
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
	wantDigest, err := k8sinventory.RenderDigest(want)
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	if got := converted.digest; got != wantDigest {
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
		if want := inventory.FromEntry(k8sinventory.NewEntry(u)); converted.entries[i] != want {
			t.Fatalf("entry %d differs from the second-export entry: got %+v, want %+v", i, converted.entries[i], want)
		}
	}

	if want := (releasesv1alpha1.InventoryEntry{
		Group: "apps", Kind: "Deployment", Version: "v1", Namespace: "default", Name: "nginx", Component: "web",
	}); converted.entries[0] != want {
		t.Fatalf("deployment entry: got %+v, want %+v", converted.entries[0], want)
	}
}

// TestRenderDigestFailure maps a failure of the library's RenderDigest to a
// render failure at the step an export failure uses.
func TestRenderDigestFailure(t *testing.T) {
	cause := errors.New("object 0: not a single JSON object")
	err := renderDigestFailure(cause)
	if err.reason != status.RenderFailedReason {
		t.Fatalf("want reason %q, got %q", status.RenderFailedReason, err.reason)
	}
	if want := "computing render digest: " + cause.Error(); err.Error() != want {
		t.Fatalf("message:\n got %q\nwant %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("the cause must stay reachable")
	}
}

// TestConvertRender_RenderDigestIgnoresTheRuntimeName pins that the render
// digest the operator records leaves out the managed-by value, so the cli
// (opm-cli) and the operator (opm-controller) digest one render alike, while
// any other label still counts.
func TestConvertRender_RenderDigestIgnoresTheRuntimeName(t *testing.T) {
	digestWith := func(labels string) string {
		t.Helper()
		src := `{apiVersion: "v1", kind: "ConfigMap", metadata: {name: "a", namespace: "x", labels: {` + labels + `}}}`
		converted, err := convertRender(&render.RenderResult{Resources: []*object.Resource{convertTestResource(t, src)}})
		if err != nil {
			t.Fatalf("convertRender: %v", err)
		}
		return converted.digest
	}
	controller := digestWith(`"app.kubernetes.io/managed-by": "opm-controller"`)
	cli := digestWith(`"app.kubernetes.io/managed-by": "opm-cli"`)
	if controller != cli {
		t.Fatalf("renders that differ only in managed-by digest differently: %q vs %q", controller, cli)
	}
	other := digestWith(`"app.kubernetes.io/managed-by": "opm-controller", app: "web"`)
	if other == controller {
		t.Fatal("a label other than managed-by must move the render digest")
	}
}

// TestInventoryDigestOf pins the inventory digest the reconcilers record:
// the library's canonical digest of the entries, independent of their order
// and sensitive to their content.
func TestInventoryDigestOf(t *testing.T) {
	a := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"}
	b := releasesv1alpha1.InventoryEntry{Kind: "Service", Namespace: "ns", Name: "svc", Version: "v1", Component: "web"}

	got := inventoryDigestOf([]releasesv1alpha1.InventoryEntry{a, b})
	if want := k8sinventory.Digest(inventory.ToEntries([]releasesv1alpha1.InventoryEntry{a, b})); got != want {
		t.Fatalf("got %q, want the library's %q", got, want)
	}
	if reversed := inventoryDigestOf([]releasesv1alpha1.InventoryEntry{b, a}); reversed != got {
		t.Fatalf("entry order moved the digest: %q vs %q", reversed, got)
	}
	renamed := b
	renamed.Name = "svc-2"
	if inventoryDigestOf([]releasesv1alpha1.InventoryEntry{a, renamed}) == got {
		t.Fatal("a changed name must move the digest")
	}
}

// TestStaleEntries pins the stale set the reconcilers prune: the library's
// component- and version-blind stale set, in previous order, never nil.
func TestStaleEntries(t *testing.T) {
	deploy := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"}
	svc := releasesv1alpha1.InventoryEntry{Kind: "Service", Namespace: "ns", Name: "svc", Version: "v1", Component: "web"}
	cm := releasesv1alpha1.InventoryEntry{Kind: "ConfigMap", Namespace: "ns", Name: "cfg", Version: "v1", Component: "web"}

	t.Run("detects stale entries", func(t *testing.T) {
		got := staleEntries([]releasesv1alpha1.InventoryEntry{deploy, svc, cm}, []releasesv1alpha1.InventoryEntry{deploy, cm})
		if len(got) != 1 || got[0] != svc {
			t.Fatalf("got %v, want only %v", got, svc)
		}
	})
	t.Run("no stale entries", func(t *testing.T) {
		got := staleEntries([]releasesv1alpha1.InventoryEntry{deploy}, []releasesv1alpha1.InventoryEntry{deploy})
		if got == nil || len(got) != 0 {
			t.Fatalf("want a non-nil empty stale set, got %#v", got)
		}
	})
	t.Run("a version change leaves nothing stale", func(t *testing.T) {
		moved := deploy
		moved.Version = "v2"
		if got := staleEntries([]releasesv1alpha1.InventoryEntry{deploy}, []releasesv1alpha1.InventoryEntry{moved}); len(got) != 0 {
			t.Fatalf("got %v, want nothing stale", got)
		}
	})
	t.Run("a component rename leaves nothing stale", func(t *testing.T) {
		renamed := deploy
		renamed.Component = "frontend"
		if got := staleEntries([]releasesv1alpha1.InventoryEntry{deploy}, []releasesv1alpha1.InventoryEntry{renamed}); len(got) != 0 {
			t.Fatalf("got %v, want nothing stale", got)
		}
	})
}
