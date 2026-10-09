package apply

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/ownership"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

const (
	guardOurs    = "11111111-1111-5111-8111-111111111111"
	guardEarlier = "22222222-2222-5222-8222-222222222222"
	guardOther   = "33333333-3333-5333-8333-333333333333"
)

// guardObject is a ConfigMap as a render or the cluster holds it.
func guardObject(name string, lbls, annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("v1")
	obj.SetKind("ConfigMap")
	obj.SetNamespace("team-a")
	obj.SetName(name)
	obj.SetLabels(lbls)
	obj.SetAnnotations(annotations)
	return obj
}

func opmLabels(uuid string) map[string]string {
	lbls := map[string]string{labels.ManagedBy: labels.ManagedByController}
	if uuid != "" {
		lbls[labels.ModuleInstanceUUID] = uuid
	}
	return lbls
}

func adoptedBy(uuid string) map[string]string {
	return map[string]string{labels.AnnotationAdopt: uuid}
}

func guardEntry(name string) releasesv1alpha1.InventoryEntry {
	return releasesv1alpha1.InventoryEntry{Kind: "ConfigMap", Version: "v1", Namespace: "team-a", Name: name}
}

// One object for each row of the design's table "Verdict and action per
// case". The expected reason is the row's; the message is the library's.
func TestGuardJudgesEveryRow(t *testing.T) {
	terminating := func(lbls, annotations map[string]string) *unstructured.Unstructured {
		obj := guardObject("x", lbls, annotations)
		now := metav1.Now()
		obj.SetDeletionTimestamp(&now)
		obj.SetFinalizers([]string{"example.com/hold"})
		return obj
	}
	tests := []struct {
		name        string
		live        *unstructured.Unstructured // nil: no live object
		inInventory bool
		want        ownership.ApplyRefusal // empty: allowed
		takenIn     bool
	}{
		{name: "row 1: no live object", live: nil},
		{name: "row 1: no live object, inventoried", live: nil, inInventory: true},
		{name: "row 2: ours, inventoried", live: guardObject("x", opmLabels(guardOurs), nil), inInventory: true},
		{name: "row 2: ours, outside the inventory", live: guardObject("x", opmLabels(guardOurs), nil), takenIn: true},
		{name: "row 3: the earlier identity's label, inventoried", live: guardObject("x", opmLabels(guardEarlier), nil), inInventory: true},
		{name: "row 4: the earlier identity's label, outside the inventory",
			live: guardObject("x", opmLabels(guardEarlier), nil), want: ownership.RefuseOtherInstance},
		{name: "row 5: another instance's label, inventoried", live: guardObject("x", opmLabels(guardOther), nil), inInventory: true},
		{name: "row 6: another instance's label, outside the inventory",
			live: guardObject("x", opmLabels(guardOther), nil), want: ownership.RefuseOtherInstance},
		{name: "row 7: no managed-by label, outside the inventory",
			live: guardObject("x", nil, nil), want: ownership.RefuseForeignObject},
		{name: "row 7: another manager, outside the inventory",
			live: guardObject("x", map[string]string{labels.ManagedBy: "Helm"}, nil), want: ownership.RefuseForeignObject},
		{name: "row 8: not OPM-managed, inventoried", live: guardObject("x", nil, nil), inInventory: true},
		{name: "row 9: OPM-managed without a UUID label, inventoried", live: guardObject("x", opmLabels(""), nil), inInventory: true},
		{name: "row 9: OPM-managed without a UUID label, outside the inventory", live: guardObject("x", opmLabels(""), nil), takenIn: true},
		{name: "row 10: annotated for this instance, foreign", live: guardObject("x", nil, adoptedBy(guardOurs)), takenIn: true},
		{name: "row 10: annotated for this instance, another instance's",
			live: guardObject("x", opmLabels(guardOther), adoptedBy(guardOurs)), takenIn: true},
		{name: "row 10: annotated for this instance, inventoried",
			live: guardObject("x", opmLabels(guardOther), adoptedBy(guardOurs)), inInventory: true},
		{name: "row 11: annotated for the earlier identity, inventoried",
			live: guardObject("x", opmLabels(guardOurs), adoptedBy(guardEarlier)), inInventory: true, want: ownership.RefuseAdoptedElsewhere},
		{name: "row 11: annotated for the earlier identity, foreign, outside the inventory",
			live: guardObject("x", nil, adoptedBy(guardEarlier)), want: ownership.RefuseForeignObject},
		{name: "row 12: annotated for another instance, inventoried",
			live: guardObject("x", opmLabels(guardOurs), adoptedBy(guardOther)), inInventory: true, want: ownership.RefuseAdoptedElsewhere},
		{name: "row 13: annotated for another instance, our label, outside the inventory",
			live: guardObject("x", opmLabels(guardOurs), adoptedBy(guardOther)), want: ownership.RefuseAdoptedElsewhere},
		{name: "row 13: annotated for another instance, its label, outside the inventory",
			live: guardObject("x", opmLabels(guardOther), adoptedBy(guardOther)), want: ownership.RefuseAdoptedElsewhere},
		{name: "row 14: annotated for another instance, foreign, outside the inventory",
			live: guardObject("x", nil, adoptedBy(guardOther)), want: ownership.RefuseForeignObject},
		{name: "row 14: annotated for another instance, a third label, outside the inventory",
			live: guardObject("x", opmLabels(guardEarlier), adoptedBy(guardOther)), want: ownership.RefuseOtherInstance},
		{name: "row 15: being deleted, inventoried", live: terminating(opmLabels(guardOurs), nil), inInventory: true, want: ownership.RefuseTerminating},
		{name: "row 15: being deleted, annotated for this instance",
			live: terminating(nil, adoptedBy(guardOurs)), want: ownership.RefuseTerminating},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder()
			if tt.live != nil {
				builder = builder.WithObjects(tt.live)
			}
			rendered := guardObject("x", opmLabels(guardOurs), nil)
			in := GuardInput{Resources: []*unstructured.Unstructured{rendered}, Identity: guardOurs}
			if tt.inInventory {
				in.Inventory = []releasesv1alpha1.InventoryEntry{guardEntry("other"), guardEntry("x")}
			}
			got, err := Guard(context.Background(), builder.Build(), in)
			if err != nil {
				t.Fatalf("Guard: %v", err)
			}

			if tt.want == "" {
				if len(got.Allowed) != 1 || got.Allowed[0] != rendered || len(got.LetGo)+len(got.Refused) != 0 {
					t.Fatalf("result = %+v, want the object allowed", got)
				}
				if (len(got.TakenIn) == 1) != tt.takenIn {
					t.Fatalf("TakenIn = %v, want taken in: %t", got.TakenIn, tt.takenIn)
				}
				return
			}

			// The library's own answer for the same input: the guard adds
			// no rule and rewords nothing.
			lib := ownership.CanApply(ownership.ApplyInput{
				Object: ownership.Object{Kind: "ConfigMap", Namespace: "team-a", Name: "x"},
				Live:   tt.live, InInventory: tt.inInventory, InstanceUUID: guardOurs,
			})
			if lib.Refuse != tt.want {
				t.Fatalf("the library says %q, the design's row says %q", lib.Refuse, tt.want)
			}
			judgedList, other := got.Refused, got.LetGo
			if tt.want == ownership.RefuseAdoptedElsewhere {
				judgedList, other = got.LetGo, got.Refused
			}
			if len(got.Allowed) != 0 || len(got.TakenIn) != 0 || len(other) != 0 || len(judgedList) != 1 {
				t.Fatalf("result = %+v, want one object under %q", got, tt.want)
			}
			j := judgedList[0]
			if j.Object != rendered || j.Refuse != tt.want || j.Message != lib.Message || j.Message == "" || j.InInventory != tt.inInventory {
				t.Fatalf("judged = %+v, want reason %q and message %q", j, tt.want, lib.Message)
			}
		})
	}
}

// The inventory match is by group, kind, namespace and name: an entry of
// another namespace, kind or group does not make the object inventoried.
func TestGuardMatchesTheInventoryByIdentity(t *testing.T) {
	live := guardObject("x", opmLabels(guardOther), nil)
	rendered := guardObject("x", opmLabels(guardOurs), nil)
	near := []releasesv1alpha1.InventoryEntry{
		{Kind: "ConfigMap", Namespace: "team-b", Name: "x"},
		{Kind: "Secret", Namespace: "team-a", Name: "x"},
		{Group: "example.com", Kind: "ConfigMap", Namespace: "team-a", Name: "x"},
	}
	c := fake.NewClientBuilder().WithObjects(live).Build()

	got, err := Guard(context.Background(), c, GuardInput{
		Resources: []*unstructured.Unstructured{rendered}, Inventory: near, Identity: guardOurs})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Refused) != 1 || got.Refused[0].Refuse != ownership.RefuseOtherInstance {
		t.Fatalf("result = %+v, want other-instance: no entry names the object", got)
	}

	// The entry's version is no part of the match.
	exact := append(near, releasesv1alpha1.InventoryEntry{Kind: "ConfigMap", Version: "v1beta1", Namespace: "team-a", Name: "x"})
	got, err = Guard(context.Background(), c, GuardInput{
		Resources: []*unstructured.Unstructured{rendered}, Inventory: exact, Identity: guardOurs})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Allowed) != 1 || len(got.TakenIn) != 0 {
		t.Fatalf("result = %+v, want the inventoried object allowed and not taken in", got)
	}
}

// The verdict is never asked with an empty identity: the library would let
// an inventoried object that is adopted elsewhere be applied.
func TestGuardRefusesAnEmptyIdentity(t *testing.T) {
	live := guardObject("x", opmLabels(guardOther), adoptedBy(guardOther))
	c := fake.NewClientBuilder().WithObjects(live).Build()
	in := GuardInput{
		Resources: []*unstructured.Unstructured{guardObject("x", nil, nil)},
		Inventory: []releasesv1alpha1.InventoryEntry{guardEntry("x")},
	}

	// What the guard must never do: with no identity the library says apply.
	if v := ownership.CanApply(ownership.ApplyInput{Live: live, InInventory: true}); !v.Allowed() {
		t.Fatalf("the library refuses with an empty identity (%q): this test no longer pins a risk", v.Refuse)
	}

	got, err := Guard(context.Background(), c, in)
	if !errors.Is(err, ErrNoIdentity) || got != nil {
		t.Fatalf("Guard = %+v, %v; want ErrNoIdentity and no result", got, err)
	}

	in.Identity = guardOurs
	got, err = Guard(context.Background(), c, in)
	if err != nil || len(got.LetGo) != 1 || len(got.Allowed) != 0 {
		t.Fatalf("Guard = %+v, %v; want the object let go", got, err)
	}
}

// The order of Allowed is the order of the apply list, and one refused
// object does not hide the others.
func TestGuardSortsAnApplyList(t *testing.T) {
	c := fake.NewClientBuilder().WithObjects(
		guardObject("foreign", nil, nil),
		guardObject("handed-over", opmLabels(guardOurs), adoptedBy(guardOther)),
		guardObject("kept", opmLabels(guardOurs), nil),
	).Build()
	names := []string{"new-1", "foreign", "kept", "handed-over", "new-2"}
	resources := make([]*unstructured.Unstructured, 0, len(names))
	for _, n := range names {
		resources = append(resources, guardObject(n, opmLabels(guardOurs), nil))
	}
	got, err := Guard(context.Background(), c, GuardInput{
		Resources: resources, Inventory: []releasesv1alpha1.InventoryEntry{guardEntry("handed-over")}, Identity: guardOurs})
	if err != nil {
		t.Fatal(err)
	}
	allowed := make([]string, 0, len(got.Allowed))
	for _, o := range got.Allowed {
		allowed = append(allowed, o.GetName())
	}
	if len(allowed) != 3 || allowed[0] != "new-1" || allowed[1] != "kept" || allowed[2] != "new-2" {
		t.Fatalf("Allowed = %v", allowed)
	}
	if len(got.TakenIn) != 1 || got.TakenIn[0].GetName() != "kept" ||
		len(got.Refused) != 1 || got.Refused[0].Object.GetName() != "foreign" ||
		len(got.LetGo) != 1 || got.LetGo[0].Object.GetName() != "handed-over" {
		t.Fatalf("result = %+v", got)
	}
}

func guardReader(get func(key client.ObjectKey, obj client.Object) error) client.Client {
	return fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := get(key, obj); err != nil {
				return err
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()
}

// A read that fails for another reason than "not found" gives no verdict.
func TestGuardFailsClosedOnAReadError(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "x", errors.New("no get"))
	c := guardReader(func(key client.ObjectKey, _ client.Object) error {
		if key.Name == "x" {
			return forbidden
		}
		return nil
	})
	got, err := Guard(context.Background(), c, GuardInput{
		Resources: []*unstructured.Unstructured{guardObject("first", nil, nil), guardObject("x", nil, nil)},
		Identity:  guardOurs,
	})
	if got != nil {
		t.Fatalf("result = %+v, want none", got)
	}
	readErr, ok := errors.AsType[*GuardReadError](err)
	if !ok || readErr.Object.Name != "x" || readErr.Object.Kind != "ConfigMap" || !apierrors.IsForbidden(err) {
		t.Fatalf("err = %v, want a GuardReadError for ConfigMap x that unwraps to Forbidden", err)
	}
}

// A kind the API server does not serve counts as "does not exist" only when
// a CustomResourceDefinition of the same list defines it.
func TestGuardAndAnUnservedKind(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	widget := &unstructured.Unstructured{}
	widget.SetGroupVersionKind(gvk)
	widget.SetNamespace("team-a")
	widget.SetName("w")
	crd := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
		"metadata": map[string]any{"name": "widgets.example.com"},
		"spec":     map[string]any{"group": "example.com", "names": map[string]any{"kind": "Widget"}},
	}}
	c := guardReader(func(_ client.ObjectKey, obj client.Object) error {
		if obj.GetObjectKind().GroupVersionKind().Kind == "Widget" {
			return &meta.NoKindMatchError{GroupKind: gvk.GroupKind(), SearchedVersions: []string{"v1"}}
		}
		return nil
	})

	got, err := Guard(context.Background(), c, GuardInput{
		Resources: []*unstructured.Unstructured{crd, widget}, Identity: guardOurs})
	if err != nil || len(got.Allowed) != 2 || len(got.TakenIn) != 0 {
		t.Fatalf("with its CRD in the list: %+v, %v; want both allowed", got, err)
	}

	got, err = Guard(context.Background(), c, GuardInput{
		Resources: []*unstructured.Unstructured{widget}, Identity: guardOurs})
	if _, ok := errors.AsType[*GuardReadError](err); !ok || got != nil {
		t.Fatalf("without its CRD: %+v, %v; want a GuardReadError", got, err)
	}
}
