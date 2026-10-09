package apply

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/ownership"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

const (
	idA = "00000000-0000-0000-0000-00000000000a"
	idB = "00000000-0000-0000-0000-00000000000b"
	idX = "00000000-0000-0000-0000-0000000000ff"
)

// liveObj builds a live object of group/kind named name in namespace
// "default" (cluster-scoped when clusterScoped), with a UID, the given
// managed-by value (none when empty), UUID label and adopt annotation.
type liveSpec struct {
	group, kind, name string
	clusterScoped     bool
	managedBy         string
	uuid              string
	adopt             string
}

func (s liveSpec) object() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: s.group, Version: "v1", Kind: s.kind})
	if !s.clusterScoped {
		obj.SetNamespace("default")
	}
	obj.SetName(s.name)
	obj.SetUID(types.UID("uid-" + s.name))
	lbls := map[string]string{}
	if s.managedBy != "" {
		lbls[labels.ManagedBy] = s.managedBy
	}
	if s.uuid != "" {
		lbls[labels.ModuleInstanceUUID] = s.uuid
	}
	obj.SetLabels(lbls)
	if s.adopt != "" {
		obj.SetAnnotations(map[string]string{labels.AnnotationAdopt: s.adopt})
	}
	return obj
}

func (s liveSpec) entry() releasesv1alpha1.InventoryEntry {
	e := releasesv1alpha1.InventoryEntry{Group: s.group, Kind: s.kind, Version: "v1", Name: s.name}
	if !s.clusterScoped {
		e.Namespace = "default"
	}
	return e
}

func own(name, uuid string) liveSpec {
	return liveSpec{kind: "ConfigMap", name: name, managedBy: labels.ManagedByController, uuid: uuid}
}

// recorder is a client that records what a prune reads and deletes.
type recorder struct {
	client.Client
	gets    []string
	deletes map[string]*metav1.Preconditions
}

func newRecorder(objs ...*unstructured.Unstructured) *recorder {
	builder := fake.NewClientBuilder()
	for _, o := range objs {
		builder = builder.WithObjects(o)
	}
	r := &recorder{deletes: map[string]*metav1.Preconditions{}}
	r.Client = interceptor.NewClient(builder.Build(), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			r.gets = append(r.gets, key.Name)
			return c.Get(ctx, key, obj, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			do := &client.DeleteOptions{}
			do.ApplyOptions(opts)
			r.deletes[obj.GetName()] = do.Preconditions
			return c.Delete(ctx, obj, opts...)
		},
	})
	return r
}

func (r *recorder) deleted(name string) bool {
	_, ok := r.deletes[name]
	return ok
}

func leftReasons(result *PruneResult) map[string]ownership.SkipReason {
	out := map[string]ownership.SkipReason{}
	for _, l := range result.Left {
		out[l.Entry.Name] = l.Reason
	}
	return out
}

// The prune judges with each identity in turn: an object that carries any of
// them is the instance's own, and an object that carries none of them is not.
func TestPruneJudgesWithEachIdentity(t *testing.T) {
	tests := []struct {
		name        string
		identities  []string
		wantDeleted []string
		wantLeft    map[string]ownership.SkipReason
	}{
		{
			name:        "one identity",
			identities:  []string{idA},
			wantDeleted: []string{"of-a", "unlabelled"},
			wantLeft:    map[string]ownership.SkipReason{"of-b": ownership.SkipOwnerMismatch, "of-x": ownership.SkipOwnerMismatch},
		},
		{
			name:        "two identities",
			identities:  []string{idB, idA},
			wantDeleted: []string{"of-a", "of-b", "unlabelled"},
			wantLeft:    map[string]ownership.SkipReason{"of-x": ownership.SkipOwnerMismatch},
		},
		{
			name:        "no identity compares none",
			identities:  nil,
			wantDeleted: []string{"of-a", "of-b", "of-x", "unlabelled"},
			wantLeft:    map[string]ownership.SkipReason{},
		},
		{
			name:        "an identity and then none",
			identities:  []string{idB, ""},
			wantDeleted: []string{"of-a", "of-b", "of-x", "unlabelled"},
			wantLeft:    map[string]ownership.SkipReason{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := []liveSpec{own("of-a", idA), own("of-b", idB), own("of-x", idX), own("unlabelled", "")}
			objs := make([]*unstructured.Unstructured, 0, len(specs))
			stale := make([]releasesv1alpha1.InventoryEntry, 0, len(specs))
			for _, s := range specs {
				objs = append(objs, s.object())
				stale = append(stale, s.entry())
			}
			c := newRecorder(objs...)

			result, err := Prune(context.Background(), c, tt.identities, stale, PruneOptions{})
			if err != nil {
				t.Fatalf("Prune: %v", err)
			}
			if result.Deleted != len(tt.wantDeleted) {
				t.Errorf("Deleted = %d, want %d", result.Deleted, len(tt.wantDeleted))
			}
			for _, name := range tt.wantDeleted {
				if !c.deleted(name) {
					t.Errorf("%s was not deleted", name)
				}
			}
			got := leftReasons(result)
			if len(got) != len(tt.wantLeft) || result.Skipped != len(tt.wantLeft) {
				t.Errorf("Left = %v (Skipped %d), want %v", got, result.Skipped, tt.wantLeft)
			}
			for name, reason := range tt.wantLeft {
				if got[name] != reason {
					t.Errorf("%s left as %q, want %q", name, got[name], reason)
				}
				if c.deleted(name) {
					t.Errorf("%s was deleted", name)
				}
			}
		})
	}
}

// An adopt annotation is judged with each identity as the UUID label is.
func TestPruneAdoptAnnotationAndIdentities(t *testing.T) {
	annotated := func(name, uuid, adopt string) liveSpec {
		s := own(name, uuid)
		s.adopt = adopt
		return s
	}
	tests := []struct {
		name       string
		spec       liveSpec
		identities []string
		want       ownership.SkipReason
	}{
		{"annotated for another instance", annotated("cm", idA, idX), []string{idA}, ownership.SkipAdoptedElsewhere},
		{"annotated for the earlier identity, not yet relabelled", annotated("cm", idA, idA), []string{idB, idA}, ""},
		// No single identity lets this delete proceed: the label is the new
		// identity's and the annotation the earlier one's.
		{"annotated for the earlier identity, relabelled", annotated("cm", idB, idA), []string{idB, idA}, ownership.SkipAdoptedElsewhere},
		{"annotated for this instance, no identity recorded", annotated("cm", "", idB), []string{idB, ""}, ""},
		{"annotated for another instance, no identity recorded", annotated("cm", "", idX), []string{idB, ""}, ownership.SkipAdoptedElsewhere},
		{"any annotation with no identity at all", annotated("cm", "", idB), nil, ownership.SkipAdoptedElsewhere},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRecorder(tt.spec.object())
			result, err := Prune(context.Background(), c, tt.identities,
				[]releasesv1alpha1.InventoryEntry{tt.spec.entry()}, PruneOptions{})
			if err != nil {
				t.Fatalf("Prune: %v", err)
			}
			if tt.want == "" {
				if !c.deleted("cm") || result.Deleted != 1 {
					t.Fatalf("object was not deleted: %+v", result)
				}
				return
			}
			if c.deleted("cm") {
				t.Fatal("object was deleted")
			}
			if got := leftReasons(result)["cm"]; got != tt.want {
				t.Fatalf("left as %q, want %q", got, tt.want)
			}
		})
	}
}

// The prune deletes exactly the entries for which the library's verdict says
// proceed, and names every other one, except one already absent, with the
// verdict's own reason and message.
func TestPruneOutcomeIsTheVerdicts(t *testing.T) {
	adopted := own("adopted", idA)
	adopted.adopt = idX
	specs := []liveSpec{
		own("proceed", idA),
		{kind: "ConfigMap", name: "foreign", managedBy: "helm"},
		own("other", idX),
		adopted,
		{kind: "Namespace", name: "ns", clusterScoped: true, managedBy: labels.ManagedByController, uuid: idA},
	}
	objs := make([]*unstructured.Unstructured, 0, len(specs))
	stale := make([]releasesv1alpha1.InventoryEntry, 0, len(specs)+1)
	for _, s := range specs {
		objs = append(objs, s.object())
		stale = append(stale, s.entry())
	}
	stale = append(stale, own("absent", idA).entry())
	c := newRecorder(objs...)

	result, err := Prune(context.Background(), c, []string{idA}, stale, PruneOptions{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if result.Deleted != 1 || len(c.deletes) != 1 || !c.deleted("proceed") {
		t.Fatalf("deleted %v (count %d), want only \"proceed\"", c.deletes, result.Deleted)
	}
	want := map[string]ownership.SkipReason{
		"foreign": ownership.SkipNotOPMManaged,
		"other":   ownership.SkipOwnerMismatch,
		"adopted": ownership.SkipAdoptedElsewhere,
		"ns":      ownership.SkipSafetyExcluded,
	}
	if result.Skipped != len(want) || len(result.Left) != len(want) {
		t.Fatalf("Skipped = %d, Left = %+v, want %d entries", result.Skipped, result.Left, len(want))
	}
	byName := map[string]liveSpec{}
	for _, s := range specs {
		byName[s.name] = s
	}
	for _, left := range result.Left {
		if want[left.Entry.Name] != left.Reason {
			t.Errorf("%s left as %q, want %q", left.Entry.Name, left.Reason, want[left.Entry.Name])
		}
		s := byName[left.Entry.Name]
		verdict := ownership.CanDelete(ownership.DeleteInput{
			Object:       ownership.Object{Group: s.group, Kind: s.kind, Namespace: left.Entry.Namespace, Name: s.name},
			Live:         s.object(),
			InstanceUUID: idA,
		})
		if left.Message != verdict.Message || left.Message == "" {
			t.Errorf("%s message = %q, want the library's %q", left.Entry.Name, left.Message, verdict.Message)
		}
	}
}

// A core Namespace and a CustomResourceDefinition of apiextensions.k8s.io are
// left without a read. The match is on group and kind: the same kind name in
// another group is deleted as any object.
func TestPruneSafetyExcludedKinds(t *testing.T) {
	coreNS := liveSpec{kind: "Namespace", name: "team-a", clusterScoped: true, managedBy: labels.ManagedByController, uuid: idA}
	crd := liveSpec{group: "apiextensions.k8s.io", kind: "CustomResourceDefinition", name: "widgets.example.com", clusterScoped: true, managedBy: labels.ManagedByController, uuid: idA}
	otherNS := liveSpec{group: "example.com", kind: "Namespace", name: "tenant", managedBy: labels.ManagedByController, uuid: idA}
	otherCRD := liveSpec{group: "example.com", kind: "CustomResourceDefinition", name: "thing", managedBy: labels.ManagedByController, uuid: idA}

	c := newRecorder(coreNS.object(), crd.object(), otherNS.object(), otherCRD.object())
	result, err := Prune(context.Background(), c, []string{idA},
		[]releasesv1alpha1.InventoryEntry{coreNS.entry(), crd.entry(), otherNS.entry(), otherCRD.entry()}, PruneOptions{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	for _, name := range []string{"team-a", "widgets.example.com"} {
		if c.deleted(name) {
			t.Errorf("%s was deleted", name)
		}
		if got := leftReasons(result)[name]; got != ownership.SkipSafetyExcluded {
			t.Errorf("%s left as %q, want safety-excluded", name, got)
		}
		for _, read := range c.gets {
			if read == name {
				t.Errorf("%s was read", name)
			}
		}
	}
	for _, name := range []string{"tenant", "thing"} {
		if !c.deleted(name) {
			t.Errorf("%s of group example.com was not deleted", name)
		}
	}
	if result.Deleted != 2 || result.Skipped != 2 {
		t.Errorf("Deleted = %d, Skipped = %d, want 2 and 2", result.Deleted, result.Skipped)
	}
}

// Every DELETE names the UID of the object the verdict judged.
func TestPruneSendsTheUIDPrecondition(t *testing.T) {
	spec := own("cm", idA)
	c := newRecorder(spec.object())

	if _, err := Prune(context.Background(), c, []string{idA},
		[]releasesv1alpha1.InventoryEntry{spec.entry()}, PruneOptions{}); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	pre := c.deletes["cm"]
	if pre == nil || pre.UID == nil || *pre.UID != "uid-cm" {
		t.Fatalf("DELETE preconditions = %+v, want UID uid-cm", pre)
	}
	if pre.ResourceVersion != nil {
		t.Fatalf("DELETE carries a resourceVersion precondition: %v", *pre.ResourceVersion)
	}
}

// A DELETE the API server answers with a Conflict was refused on its UID
// precondition: the entry fails with ErrReplaced and counts as not deleted.
func TestPruneReportsAReplacedObject(t *testing.T) {
	spec := own("cm", idA)
	other := own("other", idA)
	inner := fake.NewClientBuilder().WithObjects(spec.object(), other.object()).Build()
	c := interceptor.NewClient(inner, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetName() == "cm" {
				return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "cm", errors.New("precondition failed"))
			}
			return c.Delete(ctx, obj, opts...)
		},
	})

	result, err := Prune(context.Background(), c, []string{idA},
		[]releasesv1alpha1.InventoryEntry{spec.entry(), other.entry()}, PruneOptions{})
	if !errors.Is(err, ErrReplaced) {
		t.Fatalf("Prune error = %v, want ErrReplaced", err)
	}
	if !apierrors.IsConflict(err) {
		t.Fatalf("Prune error %v lost the API status", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("Deleted = %d, want 1: the other entry is still attempted", result.Deleted)
	}
}

// A claim counts as kept only after the verdict said proceed: a claim another
// instance holds is left behind, not kept.
func TestPruneKeepsAClaimOnlyAfterTheVerdict(t *testing.T) {
	mine := liveSpec{kind: claimKind, name: "mine", managedBy: labels.ManagedByController, uuid: idA}
	theirs := liveSpec{kind: claimKind, name: "theirs", managedBy: labels.ManagedByController, uuid: idX}
	c := newRecorder(mine.object(), theirs.object())

	result, err := Prune(context.Background(), c, []string{idA},
		[]releasesv1alpha1.InventoryEntry{mine.entry(), theirs.entry()}, PruneOptions{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(c.deletes) != 0 {
		t.Fatalf("a claim was deleted: %v", c.deletes)
	}
	if len(result.Kept) != 1 || result.Kept[0].Name != "mine" {
		t.Fatalf("Kept = %+v, want only \"mine\"", result.Kept)
	}
	if got := leftReasons(result); len(got) != 1 || got["theirs"] != ownership.SkipOwnerMismatch {
		t.Fatalf("Left = %v, want \"theirs\" as owner-mismatch", got)
	}
}

// A read that fails fails its entry and not the run. A claim that cannot be
// read while claims are kept is kept without an error.
func TestPruneFailedRead(t *testing.T) {
	claim := liveSpec{kind: claimKind, name: "data", managedBy: labels.ManagedByController, uuid: idA}
	unreadable := own("unreadable", idA)
	readable := own("readable", idA)
	inner := fake.NewClientBuilder().WithObjects(claim.object(), unreadable.object(), readable.object()).Build()
	c := interceptor.NewClient(inner, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Name != "readable" {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "x"}, key.Name, errors.New("denied"))
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	result, err := Prune(context.Background(), c, []string{idA},
		[]releasesv1alpha1.InventoryEntry{claim.entry(), unreadable.entry(), readable.entry()}, PruneOptions{})
	if err == nil || !apierrors.IsForbidden(err) {
		t.Fatalf("Prune error = %v, want the Forbidden read", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("Deleted = %d, want 1: the readable entry is still attempted", result.Deleted)
	}
	if len(result.Kept) != 1 || result.Kept[0].Name != "data" {
		t.Fatalf("Kept = %+v, want the unreadable claim", result.Kept)
	}
	if len(result.Left) != 0 {
		t.Fatalf("Left = %+v, want none: a failed read is no verdict", result.Left)
	}
}
