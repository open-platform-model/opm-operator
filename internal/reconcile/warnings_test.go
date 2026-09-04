package reconcile

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

func TestWarningTracker_Transitions(t *testing.T) {
	var tr WarningTracker
	key := types.NamespacedName{Name: "web", Namespace: metav1.NamespaceDefault}

	if tr.Update(key, nil) {
		t.Fatal("a first render with no warnings is not a transition")
	}
	if !tr.Update(key, []string{"skew on a", "trait b"}) {
		t.Fatal("the first warnings are a transition")
	}
	if tr.Update(key, []string{"trait b", "skew on a", "trait b"}) {
		t.Fatal("the same set in another order with duplicates is not a transition")
	}
	if !tr.Update(key, []string{"skew on a"}) {
		t.Fatal("a shrunk set is a transition")
	}
	if !tr.Update(key, nil) {
		t.Fatal("warnings clearing is a transition")
	}
	if tr.Update(key, nil) {
		t.Fatal("staying clear is not a transition")
	}
	if !tr.Update(key, []string{"skew on a"}) {
		t.Fatal("warnings returning after clearing is a transition")
	}

	tr.Forget(key)
	if !tr.Update(key, []string{"skew on a"}) {
		t.Fatal("after Forget the same warnings are a transition again")
	}

	var nilTracker *WarningTracker
	if !nilTracker.Update(key, []string{"x"}) || nilTracker.Update(key, nil) {
		t.Fatal("a nil tracker reports every non-empty set and never an empty one")
	}
	nilTracker.Forget(key) // must not panic
}

func TestEmitRenderWarnings_OnTransitionOnly(t *testing.T) {
	var tr WarningTracker
	mi := &releasesv1alpha1.ModuleInstance{}
	mi.Name, mi.Namespace = "web", metav1.NamespaceDefault
	key := keyOf(mi)
	rec := events.NewFakeRecorder(8)

	emitRenderWarnings(&tr, rec, mi, key, nil)
	if es := drainEvents(rec); len(es) != 0 {
		t.Fatalf("no warnings, no events; got %v", es)
	}

	warnings := []string{"version skew on \"opmodel.dev/catalogs/opm@v4\": module requires v4.1.0, platform carries v4.0.1", "unhandled optional trait"}
	emitRenderWarnings(&tr, rec, mi, key, warnings)
	es := drainEvents(rec)
	if len(es) != 2 || countEventsWithReason(es, status.RenderWarningReason) != 2 {
		t.Fatalf("want one RenderWarning event per distinct warning, got %v", es)
	}

	emitRenderWarnings(&tr, rec, mi, key, warnings)
	if es := drainEvents(rec); len(es) != 0 {
		t.Fatalf("an unchanged set must not re-emit; got %v", es)
	}
}
