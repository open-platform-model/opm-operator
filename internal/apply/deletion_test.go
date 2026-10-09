package apply

import (
	"context"
	"errors"
	"slices"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"
	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/lifecycle"
	"github.com/open-platform-model/library/opm/k8s/ownership"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// request is one read or delete a deletion sent.
type request struct {
	verb        string
	name        string
	propagation metav1.DeletionPropagation
	uid         types.UID
}

// requestLog is a client that records, in order, every read and delete a
// deletion sends, and can fail the read or the delete of a named object.
type requestLog struct {
	client.Client
	requests   []request
	failGet    map[string]error
	failDelete map[string]error
	// misread makes the read of a name return the object of another name.
	misread map[string]string
}

func newRequestLog(objs ...*unstructured.Unstructured) *requestLog {
	builder := fake.NewClientBuilder()
	for _, o := range objs {
		builder = builder.WithObjects(o)
	}
	l := &requestLog{failGet: map[string]error{}, failDelete: map[string]error{}, misread: map[string]string{}}
	l.Client = interceptor.NewClient(builder.Build(), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			l.requests = append(l.requests, request{verb: "get", name: key.Name})
			if err := l.failGet[key.Name]; err != nil {
				return err
			}
			if other, ok := l.misread[key.Name]; ok {
				key.Name = other
			}
			return c.Get(ctx, key, obj, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			do := &client.DeleteOptions{}
			do.ApplyOptions(opts)
			r := request{verb: "delete", name: obj.GetName()}
			if do.PropagationPolicy != nil {
				r.propagation = *do.PropagationPolicy
			}
			if do.Preconditions != nil && do.Preconditions.UID != nil {
				r.uid = *do.Preconditions.UID
			}
			l.requests = append(l.requests, r)
			if err := l.failDelete[obj.GetName()]; err != nil {
				return err
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	return l
}

func (l *requestLog) deletes() []request {
	var out []request
	for _, r := range l.requests {
		if r.verb == "delete" {
			out = append(out, r)
		}
	}
	return out
}

func (l *requestLog) deletedNames() []string {
	deletes := l.deletes()
	out := make([]string, 0, len(deletes))
	for _, r := range deletes {
		out = append(out, r.name)
	}
	return out
}

func planOf(identity string, specs ...liveSpec) lifecycle.DeletionPlan {
	entries := make([]k8sinventory.Entry, 0, len(specs))
	for _, s := range specs {
		entries = append(entries, k8sinventory.Entry(s.entry()))
	}
	return lifecycle.NewDeletionPlan(entries, lifecycle.Policy{Prune: true}, identity)
}

var (
	errForbidden = apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "cm", errors.New("denied"))
	errConflict  = apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "cm", errors.New("precondition failed"))
	errNotFound  = apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "cm")
	errServer    = apierrors.NewInternalError(errors.New("etcd is down"))
)

// The runner performs what Advance names and records, for every outcome a
// step can have, the library's outcome, the action that failed, the raw
// error and the object that was read.
func TestRunDeletionRecordsEveryOutcome(t *testing.T) {
	adopted := own("cm", idA)
	adopted.adopt = idX

	tests := []struct {
		name       string
		spec       liveSpec
		absent     bool
		failGet    error
		failDelete error
		misread    bool

		wantResult  lifecycle.Result
		wantSkip    ownership.SkipReason
		wantFailure lifecycle.FailureClass
		wantFailed  lifecycle.ActionKind
		wantDelete  bool
		wantLive    bool
		wantErr     func(error) bool
	}{
		{name: "deleted", spec: own("cm", idA),
			wantResult: lifecycle.ResultDeleted, wantDelete: true, wantLive: true},
		{name: "already absent at the read", spec: own("cm", idA), absent: true,
			wantResult: lifecycle.ResultSkipped, wantSkip: ownership.SkipAlreadyAbsent},
		{name: "gone between the read and the delete", spec: own("cm", idA), failDelete: errNotFound,
			wantResult: lifecycle.ResultSkipped, wantSkip: ownership.SkipAlreadyAbsent, wantDelete: true, wantLive: true},
		{name: "a kind OPM never deletes",
			spec:       liveSpec{kind: "Namespace", name: "cm", clusterScoped: true, managedBy: labels.ManagedByController, uuid: idA},
			wantResult: lifecycle.ResultSkipped, wantSkip: ownership.SkipSafetyExcluded},
		{name: "not managed by OPM", spec: liveSpec{kind: "ConfigMap", name: "cm", managedBy: "helm"},
			wantResult: lifecycle.ResultSkipped, wantSkip: ownership.SkipNotOPMManaged, wantLive: true},
		{name: "another instance's", spec: own("cm", idX),
			wantResult: lifecycle.ResultSkipped, wantSkip: ownership.SkipOwnerMismatch, wantLive: true},
		{name: "adopted by another instance", spec: adopted,
			wantResult: lifecycle.ResultSkipped, wantSkip: ownership.SkipAdoptedElsewhere, wantLive: true},
		{name: "failed read", spec: own("cm", idA), failGet: errServer,
			wantResult: lifecycle.ResultFailed, wantFailure: lifecycle.FailureError, wantFailed: lifecycle.ActionRead,
			wantErr: apierrors.IsInternalError},
		{name: "forbidden read", spec: own("cm", idA), failGet: errForbidden,
			wantResult: lifecycle.ResultFailed, wantFailure: lifecycle.FailureForbidden, wantFailed: lifecycle.ActionRead,
			wantErr: apierrors.IsForbidden},
		{name: "forbidden delete", spec: own("cm", idA), failDelete: errForbidden,
			wantResult: lifecycle.ResultFailed, wantFailure: lifecycle.FailureForbidden, wantFailed: lifecycle.ActionDelete,
			wantDelete: true, wantLive: true, wantErr: apierrors.IsForbidden},
		{name: "conflict on the UID precondition", spec: own("cm", idA), failDelete: errConflict,
			wantResult: lifecycle.ResultFailed, wantFailure: lifecycle.FailureConflict, wantFailed: lifecycle.ActionDelete,
			wantDelete: true, wantLive: true,
			wantErr: func(err error) bool { return errors.Is(err, ErrReplaced) && apierrors.IsConflict(err) }},
		{name: "a read that returns another object", spec: own("cm", idA), misread: true,
			wantResult: lifecycle.ResultFailed, wantFailure: lifecycle.FailureError, wantFailed: lifecycle.ActionRead,
			wantLive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []*unstructured.Unstructured
			if !tt.absent {
				objs = append(objs, tt.spec.object())
			}
			objs = append(objs, own("someone-else", idA).object())
			c := newRequestLog(objs...)
			if tt.failGet != nil {
				c.failGet["cm"] = tt.failGet
			}
			if tt.failDelete != nil {
				c.failDelete["cm"] = tt.failDelete
			}
			if tt.misread {
				c.misread["cm"] = "someone-else"
			}

			plan := planOf(idA, tt.spec)
			run, err := runDeletion(context.Background(), c, plan)
			if err != nil {
				t.Fatalf("runDeletion: %v", err)
			}

			if len(run.Steps) != 1 || run.State.Next != 1 || len(run.State.Outcomes) != 1 {
				t.Fatalf("run = %+v, want one finished step", run)
			}
			step := run.Steps[0]
			if step.Entry != tt.spec.entry() {
				t.Errorf("Entry = %+v, want %+v", step.Entry, tt.spec.entry())
			}
			if step.Outcome != run.State.Outcomes[0] {
				t.Errorf("Outcome = %+v, want the state's %+v", step.Outcome, run.State.Outcomes[0])
			}
			if step.Outcome.Result != tt.wantResult || step.Outcome.Skip != tt.wantSkip || step.Outcome.Failure != tt.wantFailure {
				t.Errorf("Outcome = %+v, want result %q skip %q failure %q",
					step.Outcome, tt.wantResult, tt.wantSkip, tt.wantFailure)
			}
			if step.Failed != tt.wantFailed {
				t.Errorf("Failed = %q, want %q", step.Failed, tt.wantFailed)
			}
			if (step.Live != nil) != tt.wantLive {
				t.Errorf("Live = %v, want set: %v", step.Live, tt.wantLive)
			}
			switch {
			case tt.wantErr == nil && step.Err != nil:
				t.Errorf("Err = %v, want none", step.Err)
			case tt.wantErr != nil && !tt.wantErr(step.Err):
				t.Errorf("Err = %v, not the error of the failed action", step.Err)
			}
			if got := len(c.deletes()) == 1; got != tt.wantDelete {
				t.Errorf("deletes = %+v, want a delete: %v", c.deletes(), tt.wantDelete)
			}
			if tt.misread && step.Outcome.Message == "" {
				t.Error("a misread step carries no message")
			}
			// The object of another name is never touched.
			if slices.Contains(c.deletedNames(), "someone-else") {
				t.Error("an object outside the plan was deleted")
			}
		})
	}
}

// A failed step does not stop the plan: the steps after it are still read
// and deleted.
func TestRunDeletionGoesOnAfterAFailure(t *testing.T) {
	first, second, third := own("first", idA), own("second", idA), own("third", idA)
	c := newRequestLog(first.object(), second.object(), third.object())
	c.failGet["first"] = errForbidden
	c.failDelete["second"] = errServer

	run, err := runDeletion(context.Background(), c, planOf(idA, first, second, third))
	if err != nil {
		t.Fatalf("runDeletion: %v", err)
	}
	got := []lifecycle.Result{run.Steps[0].Outcome.Result, run.Steps[1].Outcome.Result, run.Steps[2].Outcome.Result}
	want := []lifecycle.Result{lifecycle.ResultFailed, lifecycle.ResultFailed, lifecycle.ResultDeleted}
	if !slices.Equal(got, want) {
		t.Fatalf("results = %v, want %v", got, want)
	}
	if run.Steps[0].Failed != lifecycle.ActionRead || run.Steps[1].Failed != lifecycle.ActionDelete {
		t.Fatalf("failed actions = %q, %q, want read, delete", run.Steps[0].Failed, run.Steps[1].Failed)
	}
	if !apierrors.IsForbidden(run.Steps[0].Err) || !apierrors.IsInternalError(run.Steps[1].Err) {
		t.Fatalf("errors = %v, %v: each step keeps its own", run.Steps[0].Err, run.Steps[1].Err)
	}
}

// A plan whose policy does not prune names no action: nothing is read or
// deleted, and no step has an outcome.
func TestRunDeletionWithoutPrune(t *testing.T) {
	spec := own("cm", idA)
	c := newRequestLog(spec.object())
	plan := lifecycle.NewDeletionPlan([]k8sinventory.Entry{k8sinventory.Entry(spec.entry())}, lifecycle.Policy{}, idA)

	run, err := runDeletion(context.Background(), c, plan)
	if err != nil {
		t.Fatalf("runDeletion: %v", err)
	}
	if len(c.requests) != 0 {
		t.Fatalf("requests = %+v, want none", c.requests)
	}
	if len(run.Steps) != 1 || run.Steps[0].Outcome.Result != "" {
		t.Fatalf("steps = %+v, want one step without an outcome", run.Steps)
	}
}

// A plan over entries in inventory order sends its DELETEs in descending kind
// weight, each with Foreground propagation and the UID of the object read.
func TestRunDeletionOrderAndRequest(t *testing.T) {
	managed := func(group, kind, name string) liveSpec {
		return liveSpec{group: group, kind: kind, name: name, managedBy: labels.ManagedByController, uuid: idA}
	}
	// Inventory order: the order an apply wrote them in, lightest first.
	specs := []liveSpec{
		managed("", "ConfigMap", "config"),
		managed("", "Service", "service"),
		managed("apps", "Deployment", "deployment"),
		managed("example.com", "Widget", "widget"),
		managed("", "ConfigMap", "config-2"),
	}
	objs := make([]*unstructured.Unstructured, 0, len(specs))
	for _, s := range specs {
		objs = append(objs, s.object())
	}
	c := newRequestLog(objs...)

	if _, err := runDeletion(context.Background(), c, planOf(idA, specs...)); err != nil {
		t.Fatalf("runDeletion: %v", err)
	}

	// A kind without a weight of its own goes first, then the workload, the
	// Service, and the ConfigMaps in their inventory order.
	want := []string{"widget", "deployment", "service", "config", "config-2"}
	if got := c.deletedNames(); !slices.Equal(got, want) {
		t.Fatalf("DELETE order = %v, want %v", got, want)
	}
	for _, r := range c.deletes() {
		if r.propagation != metav1.DeletePropagationForeground {
			t.Errorf("DELETE of %s carries propagation %q, want Foreground", r.name, r.propagation)
		}
		if r.uid != types.UID("uid-"+r.name) {
			t.Errorf("DELETE of %s carries UID %q, want the UID of the object read", r.name, r.uid)
		}
	}
	// Each object is read before it is deleted, and nothing is read twice.
	wantRequests := make([]string, 0, 2*len(want))
	for _, name := range want {
		wantRequests = append(wantRequests, "get "+name, "delete "+name)
	}
	gotRequests := make([]string, 0, len(c.requests))
	for _, r := range c.requests {
		gotRequests = append(gotRequests, r.verb+" "+r.name)
	}
	if !slices.Equal(gotRequests, wantRequests) {
		t.Fatalf("requests = %v, want %v", gotRequests, wantRequests)
	}
}

func resultsByName(d Deletion) map[string]StepResult {
	out := map[string]StepResult{}
	for _, r := range d.Results() {
		out[r.Entry.Name] = r
	}
	return out
}

// The driver judges with each identity in turn, as the prune did: the cases
// of TestPruneJudgesWithEachIdentity, asked of the plans.
func TestRunDeletionJudgesWithEachIdentity(t *testing.T) {
	tests := []struct {
		name        string
		identities  []string
		wantPlans   int
		wantDeleted []string
		wantLeft    []string
	}{
		{"one identity", []string{idA}, 1, []string{"of-a", "unlabelled"}, []string{"of-b", "of-x"}},
		{"two identities", []string{idB, idA}, 2, []string{"of-a", "of-b", "unlabelled"}, []string{"of-x"}},
		{"no identity compares none", nil, 1, []string{"of-a", "of-b", "of-x", "unlabelled"}, nil},
		{"an identity and then none", []string{idB, ""}, 2, []string{"of-a", "of-b", "of-x", "unlabelled"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := []liveSpec{own("of-a", idA), own("of-b", idB), own("of-x", idX), own("unlabelled", "")}
			objs := make([]*unstructured.Unstructured, 0, len(specs))
			entries := make([]releasesv1alpha1.InventoryEntry, 0, len(specs))
			for _, s := range specs {
				objs = append(objs, s.object())
				entries = append(entries, s.entry())
			}
			c := newRequestLog(objs...)

			deletion, err := RunDeletion(context.Background(), c, entries, tt.identities, lifecycle.Policy{Prune: true})
			if err != nil {
				t.Fatalf("RunDeletion: %v", err)
			}
			if len(deletion.Runs) != tt.wantPlans {
				t.Fatalf("plans = %d, want %d", len(deletion.Runs), tt.wantPlans)
			}

			deleted := c.deletedNames()
			slices.Sort(deleted)
			if !slices.Equal(deleted, tt.wantDeleted) {
				t.Fatalf("deleted %v, want %v: no entry is deleted by two plans", deleted, tt.wantDeleted)
			}
			results := resultsByName(deletion)
			if len(results) != len(specs) {
				t.Fatalf("results = %+v, want one per entry", results)
			}
			for _, name := range tt.wantDeleted {
				if results[name].Outcome.Result != lifecycle.ResultDeleted {
					t.Errorf("%s = %+v, want deleted", name, results[name].Outcome)
				}
			}
			for _, name := range tt.wantLeft {
				if results[name].Outcome.Skip != ownership.SkipOwnerMismatch {
					t.Errorf("%s = %+v, want owner-mismatch", name, results[name].Outcome)
				}
			}
		})
	}
}

// An adopt annotation is judged with each identity as the UUID label is: the
// cases of TestPruneAdoptAnnotationAndIdentities, asked of the plans.
func TestRunDeletionAdoptAnnotationAndIdentities(t *testing.T) {
	annotated := func(uuid, adopt string) liveSpec {
		s := own("cm", uuid)
		s.adopt = adopt
		return s
	}
	tests := []struct {
		name       string
		spec       liveSpec
		identities []string
		want       ownership.SkipReason
	}{
		{"annotated for another instance", annotated(idA, idX), []string{idA}, ownership.SkipAdoptedElsewhere},
		{"annotated for the earlier identity, not yet relabelled", annotated(idA, idA), []string{idB, idA}, ""},
		{"annotated for the earlier identity, relabelled", annotated(idB, idA), []string{idB, idA}, ownership.SkipAdoptedElsewhere},
		{"annotated for this instance, no identity recorded", annotated("", idB), []string{idB, ""}, ""},
		{"annotated for another instance, no identity recorded", annotated("", idX), []string{idB, ""}, ownership.SkipAdoptedElsewhere},
		{"any annotation with no identity at all", annotated("", idB), nil, ownership.SkipAdoptedElsewhere},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRequestLog(tt.spec.object())
			deletion, err := RunDeletion(context.Background(), c,
				[]releasesv1alpha1.InventoryEntry{tt.spec.entry()}, tt.identities, lifecycle.Policy{Prune: true})
			if err != nil {
				t.Fatalf("RunDeletion: %v", err)
			}
			got := resultsByName(deletion)["cm"].Outcome
			if tt.want == "" {
				if got.Result != lifecycle.ResultDeleted || len(c.deletes()) != 1 {
					t.Fatalf("outcome = %+v, deletes = %+v, want one delete", got, c.deletes())
				}
				return
			}
			if len(c.deletes()) != 0 {
				t.Fatalf("object was deleted: %+v", c.deletes())
			}
			if got.Skip != tt.want {
				t.Fatalf("skipped as %q, want %q", got.Skip, tt.want)
			}
		})
	}
}

// An entry no plan deletes keeps the reason and message of the first plan,
// and an entry a later plan fails on reports that failure.
func TestRunDeletionReportsTheFirstVerdict(t *testing.T) {
	// The label is the new identity's and the annotation the earlier one's:
	// the first plan says adopted-elsewhere, the second owner-mismatch.
	torn := own("torn", idB)
	torn.adopt = idA
	failing := own("failing", idA)
	c := newRequestLog(torn.object(), failing.object())
	c.failDelete["failing"] = errForbidden

	deletion, err := RunDeletion(context.Background(), c,
		[]releasesv1alpha1.InventoryEntry{torn.entry(), failing.entry()}, []string{idB, idA}, lifecycle.Policy{Prune: true})
	if err != nil {
		t.Fatalf("RunDeletion: %v", err)
	}
	if len(deletion.Runs) != 2 || len(deletion.Runs[1].Steps) != 2 {
		t.Fatalf("runs = %+v, want a second plan over both entries", deletion.Runs)
	}
	if got := deletion.Runs[1].Plan.OwnerUUID(); got != idA {
		t.Fatalf("second plan judges with %q, want the earlier identity", got)
	}
	results := resultsByName(deletion)

	first := ownership.CanDelete(ownership.DeleteInput{
		Object:       ownership.Object{Kind: "ConfigMap", Namespace: "default", Name: "torn"},
		Live:         torn.object(),
		InstanceUUID: idB,
	})
	if got := results["torn"].Outcome; got.Skip != first.Skip || got.Message != first.Message || got.Message == "" {
		t.Errorf("torn = %+v, want the first verdict (%q, %q)", got, first.Skip, first.Message)
	}
	if got := results["failing"]; got.Outcome.Result != lifecycle.ResultFailed ||
		got.Failed != lifecycle.ActionDelete || !apierrors.IsForbidden(got.Err) {
		t.Errorf("failing = %+v, want the failed delete of the second plan", got)
	}
}

// Kept claims are taken out before the plan and classified by a pass that
// reads and never deletes.
func TestKeptClaimsAreSplitAndClassified(t *testing.T) {
	claim := func(name, uuid string) liveSpec {
		return liveSpec{kind: claimKind, name: name, managedBy: labels.ManagedByController, uuid: uuid}
	}
	mine, earlier, theirs := claim("mine", idA), claim("earlier", idB), claim("theirs", idX)
	gone, unreadable := claim("gone", idA), claim("unreadable", idA)
	foreign := liveSpec{kind: claimKind, name: "foreign", managedBy: "helm"}
	other := liveSpec{group: "example.com", kind: claimKind, name: "other-group", managedBy: labels.ManagedByController, uuid: idA}
	cm := own("cm", idA)

	entries := []releasesv1alpha1.InventoryEntry{
		cm.entry(), mine.entry(), earlier.entry(), theirs.entry(), gone.entry(), unreadable.entry(), foreign.entry(), other.entry(),
	}

	t.Run("claims are planned when data may be deleted", func(t *testing.T) {
		planned, claims := SplitKeptClaims(entries, true)
		if len(planned) != len(entries) || len(claims) != 0 {
			t.Fatalf("planned %d, claims %d, want every entry planned", len(planned), len(claims))
		}
	})

	planned, claims := SplitKeptClaims(entries, false)
	if len(planned) != 2 || planned[0] != cm.entry() || planned[1] != other.entry() {
		t.Fatalf("planned = %+v, want the ConfigMap and the claim of another group", planned)
	}
	if len(claims) != 6 {
		t.Fatalf("claims = %+v, want the six core claims", claims)
	}

	c := newRequestLog(mine.object(), earlier.object(), theirs.object(), unreadable.object(), foreign.object())
	c.failGet["unreadable"] = errForbidden

	kept, left := ClassifyKeptClaims(context.Background(), c, []string{idA, idB}, claims)

	keptNames := make([]string, 0, len(kept))
	for _, e := range kept {
		keptNames = append(keptNames, e.Name)
	}
	if want := []string{"mine", "earlier", "unreadable"}; !slices.Equal(keptNames, want) {
		t.Errorf("kept = %v, want %v", keptNames, want)
	}
	leftReasons := map[string]ownership.SkipReason{}
	for _, l := range left {
		leftReasons[l.Entry.Name] = l.Reason
		if l.Message == "" {
			t.Errorf("%s is left without the verdict's message", l.Entry.Name)
		}
	}
	if len(left) != 2 || leftReasons["theirs"] != ownership.SkipOwnerMismatch || leftReasons["foreign"] != ownership.SkipNotOPMManaged {
		t.Errorf("left = %v, want theirs as owner-mismatch and foreign as not-opm-managed", leftReasons)
	}
	if len(c.deletes()) != 0 {
		t.Fatalf("the classification sent a DELETE: %+v", c.deletes())
	}
	if len(c.requests) != len(claims) {
		t.Fatalf("requests = %+v, want one read per claim", c.requests)
	}
}
