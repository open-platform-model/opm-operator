package status

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

const foreground = metav1.FinalizerDeleteDependents

func deletingInstance() *releasesv1alpha1.ModuleInstance {
	now := metav1.Now()
	return &releasesv1alpha1.ModuleInstance{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}}
}

func conditionOf(obj conditions.Getter, kind string) *metav1.Condition {
	return apimeta.FindStatusCondition(obj.GetConditions(), kind)
}

func TestMarkDeletionInProgressIsNotStalled(t *testing.T) {
	mi := deletingInstance()
	MarkStalled(mi, DeletionSAMissingReason, "earlier stall")
	MarkDeletionInProgress(mi, "waiting")

	ready := conditionOf(mi, ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != DeletionInProgressReason {
		t.Fatalf("Ready = %+v, want False with DeletionInProgress", ready)
	}
	reconciling := conditionOf(mi, ReconcilingCondition)
	if reconciling == nil || reconciling.Status != metav1.ConditionTrue || reconciling.Reason != DeletionInProgressReason {
		t.Errorf("Reconciling = %+v, want True with DeletionInProgress", reconciling)
	}
	if stalled := conditionOf(mi, StalledCondition); stalled != nil {
		t.Errorf("Stalled = %+v, want absent", stalled)
	}
}

func TestMarkDeletionBlockedIsStalled(t *testing.T) {
	mi := deletingInstance()
	MarkDeletionInProgress(mi, "waiting")
	MarkDeletionBlocked(mi, "blocked")

	ready := conditionOf(mi, ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != DeletionBlockedReason {
		t.Fatalf("Ready = %+v, want False with DeletionBlocked", ready)
	}
	stalled := conditionOf(mi, StalledCondition)
	if stalled == nil || stalled.Status != metav1.ConditionTrue || stalled.Reason != DeletionBlockedReason {
		t.Errorf("Stalled = %+v, want True with DeletionBlocked", stalled)
	}
	if reconciling := conditionOf(mi, ReconcilingCondition); reconciling != nil {
		t.Errorf("Reconciling = %+v, want absent", reconciling)
	}
}

// Neither reason appears on an object that is not being deleted.
func TestDeletionWaitReasonsOnlyOnADeletingObject(t *testing.T) {
	mi := &releasesv1alpha1.ModuleInstance{}
	MarkDeletionInProgress(mi, "waiting")
	MarkDeletionBlocked(mi, "blocked")
	if len(mi.Status.Conditions) != 0 {
		t.Fatalf("conditions = %+v, want none on a live object", mi.Status.Conditions)
	}
}

func TestWaitingObjectNamesWhatHoldsIt(t *testing.T) {
	cases := []struct {
		name       string
		finalizers []string
		want       string
	}{
		{"only the collector's finalizer", []string{foreground},
			"Deployment/media/jellyfin (waits for its dependents to be deleted)"},
		{"another finalizer is named first", []string{foreground, "example.com/hold"},
			"Deployment/media/jellyfin (finalizers: example.com/hold, " + foreground + ")"},
		{"at most three finalizers, the rest counted",
			[]string{foreground, "example.com/a", "example.com/b", "example.com/c", "example.com/d"},
			"Deployment/media/jellyfin (finalizers: example.com/a, example.com/b, example.com/c and 2 more)"},
		{"no finalizer", nil, "Deployment/media/jellyfin (terminating, no finalizer)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WaitingObject{Kind: "Deployment", Namespace: "media", Name: "jellyfin", Finalizers: tc.finalizers}.String()
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
	if got := (WaitingObject{Kind: "ClusterRole", Name: "reader", Finalizers: []string{foreground}}).String(); !strings.HasPrefix(got, "ClusterRole/reader (") {
		t.Errorf("a cluster-scoped object is named without a namespace, got %q", got)
	}
}

func waiting(n int) []WaitingObject {
	out := make([]WaitingObject, n)
	for i := range out {
		out[i] = WaitingObject{Kind: "ConfigMap", Namespace: "media", Name: fmt.Sprintf("cm-%02d", i), Finalizers: []string{"example.com/hold"}}
	}
	return out
}

func TestDeletionNotesCutALongList(t *testing.T) {
	for name, note := range map[string]string{
		"in progress": DeletionInProgressNote(waiting(15)),
		"blocked":     DeletionBlockedNote(waiting(15), 10*time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			if got := strings.Count(note, "ConfigMap/media/cm-"); got != 10 {
				t.Errorf("the note names %d objects, want 10: %s", got, note)
			}
			if !strings.Contains(note, "and 5 more") || !strings.Contains(note, "15 ") {
				t.Errorf("the note must count 15 objects and say that 5 more remain: %s", note)
			}
		})
	}
}

func TestDeletionBlockedNoteNamesTheWaysOut(t *testing.T) {
	note := DeletionBlockedNote([]WaitingObject{
		{Kind: "Deployment", Namespace: "media", Name: "jellyfin", Finalizers: []string{foreground}},
	}, 10*time.Minute)
	for _, want := range []string{
		"Deployment/media/jellyfin (waits for its dependents to be deleted)",
		"10m0s",
		"spec.prune=false",
		"delete the dependent that cannot stop",
		releasesv1alpha1.AnnotationForceDeleteOrphan + " does not release this wait",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not contain %q: %s", want, note)
		}
	}
}

func TestDeletionUnconfirmedNotes(t *testing.T) {
	cases := []struct {
		note string
		want []string
	}{
		{DeletionUnconfirmedNote(3, "media/deploy", UnreadIdentityMissing),
			[]string{"3 object(s)", `ServiceAccount "media/deploy" is missing`, "may still exist"}},
		{DeletionUnconfirmedNote(1, "media/deploy", UnreadIdentityFailed),
			[]string{`ServiceAccount "media/deploy" cannot be impersonated`}},
		{DeletionUnconfirmedNote(2, "media/deploy", UnreadForbidden),
			[]string{`ServiceAccount "media/deploy" is forbidden to read them`}},
		{DeletionUnconfirmedNote(2, "", UnreadForbidden), []string{"the controller is forbidden to read them"}},
		{ClaimsUnreadNote(2, "media/deploy", UnreadIdentityMissing),
			[]string{"2 PersistentVolumeClaim(s)", `ServiceAccount "media/deploy" is missing`, "left in place", "may still exist"}},
	}
	for _, tc := range cases {
		for _, want := range tc.want {
			if !strings.Contains(tc.note, want) {
				t.Errorf("the note does not contain %q: %s", want, tc.note)
			}
		}
		if strings.Contains(tc.note, "0012") {
			t.Errorf("the note carries an enhancement reference: %s", tc.note)
		}
	}
}

func TestEventNoteIsCutToTheLimit(t *testing.T) {
	long := strings.Repeat("x", 2000)
	if got := EventNote(long); len(got) != eventNoteLimit || !strings.HasSuffix(got, "...") {
		t.Errorf("len = %d, want %d ending in dots", len(got), eventNoteLimit)
	}
	if got := EventNote("short"); got != "short" {
		t.Errorf("a short note changed: %q", got)
	}
}
