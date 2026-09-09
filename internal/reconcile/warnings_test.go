package reconcile

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"

	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/render"
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

// webInstance returns a ModuleInstance named web in the default namespace
// and its tracker key.
func webInstance() (*releasesv1alpha1.ModuleInstance, types.NamespacedName) {
	mi := &releasesv1alpha1.ModuleInstance{}
	mi.Name, mi.Namespace = "web", metav1.NamespaceDefault
	return mi, keyOf(mi)
}

// advisoryResult builds a render result carrying one skew row (Newer) and
// one unhandled optional trait, worded as warnings. The wording is a
// parameter so tests can vary it independently of the facts.
func advisoryResult(platformVersion string, warnings ...string) *render.RenderResult {
	return &render.RenderResult{
		Warnings: warnings,
		UnhandledTraits: map[string][]string{
			"web": {"opmodel.dev/catalogs/opm/traits/expose@v4"},
		},
		ResolvedVersions: []kernel.ResolvedVersion{
			{Path: "opmodel.dev/core@v2", ModuleVersion: "v2.0.0", PlatformVersion: "v2.0.0"},
			{Path: "opmodel.dev/catalogs/opm@v4", ModuleVersion: "v4.1.0", PlatformVersion: platformVersion, Newer: true},
		},
	}
}

func TestEmitRenderWarnings_OnTransitionOnly(t *testing.T) {
	var tr WarningTracker
	mi, key := webInstance()
	rec := events.NewFakeRecorder(8)

	emitRenderWarnings(&tr, rec, mi, key, &render.RenderResult{})
	if es := drainEvents(rec); len(es) != 0 {
		t.Fatalf("no warnings, no events; got %v", es)
	}

	result := advisoryResult("v4.0.1",
		"version skew on \"opmodel.dev/catalogs/opm@v4\": module requires v4.1.0, platform carries v4.0.1",
		"unhandled optional trait")
	emitRenderWarnings(&tr, rec, mi, key, result)
	es := drainEvents(rec)
	if len(es) != 2 || countEventsWithReason(es, status.RenderWarningReason) != 2 {
		t.Fatalf("want one RenderWarning event per distinct warning, got %v", es)
	}

	emitRenderWarnings(&tr, rec, mi, key, result)
	if es := drainEvents(rec); len(es) != 0 {
		t.Fatalf("an unchanged set must not re-emit; got %v", es)
	}
}

// The transition check is keyed on the advisory facts, not on the worded
// text: the same rows worded differently are an unchanged set. A tracker
// keyed on the strings would re-emit here.
func TestEmitRenderWarnings_RewordingDoesNotReemit(t *testing.T) {
	var tr WarningTracker
	mi, key := webInstance()
	rec := events.NewFakeRecorder(8)

	emitRenderWarnings(&tr, rec, mi, key, advisoryResult("v4.0.1", "skew, worded one way", "trait, worded one way"))
	if es := drainEvents(rec); len(es) != 2 {
		t.Fatalf("the first warnings are a transition; got %v", es)
	}

	emitRenderWarnings(&tr, rec, mi, key, advisoryResult("v4.0.1", "skew, worded another way", "trait, worded another way"))
	if es := drainEvents(rec); len(es) != 0 {
		t.Fatalf("rewording unchanged facts must not re-emit; got %v", es)
	}
}

// A changed fact is a transition even when the worded text happens to be
// identical. A tracker keyed on the strings would stay silent here.
func TestEmitRenderWarnings_ChangedFactEmits(t *testing.T) {
	var tr WarningTracker
	mi, key := webInstance()
	rec := events.NewFakeRecorder(8)

	const sameText = "catalog skew; see the resolved-versions rows"
	emitRenderWarnings(&tr, rec, mi, key, advisoryResult("v4.0.1", sameText))
	if es := drainEvents(rec); len(es) != 1 {
		t.Fatalf("the first warning is a transition; got %v", es)
	}

	// The platform moved to another build the module is still newer than:
	// the skew row's facts changed, the text did not.
	emitRenderWarnings(&tr, rec, mi, key, advisoryResult("v4.0.2", sameText))
	if es := drainEvents(rec); len(es) != 1 || countEventsWithReason(es, status.RenderWarningReason) != 1 {
		t.Fatalf("a changed fact under unchanged wording must emit; got %v", es)
	}
}

// advisoryFacts keys a skew row on the path and both versions and a trait on
// the component and the trait; rows the policy did not flag contribute
// nothing, and a nil result has no facts.
func TestAdvisoryFacts(t *testing.T) {
	facts := distinctSorted(advisoryFacts(advisoryResult("v4.0.1", "any wording")))
	want := []string{
		"skew opmodel.dev/catalogs/opm@v4 v4.1.0 v4.0.1",
		"trait web opmodel.dev/catalogs/opm/traits/expose@v4",
	}
	if len(facts) != len(want) {
		t.Fatalf("want %v, got %v", want, facts)
	}
	for i := range want {
		if facts[i] != want[i] {
			t.Fatalf("want %v, got %v", want, facts)
		}
	}
	if got := advisoryFacts(nil); len(got) != 0 {
		t.Fatalf("a nil result has no facts; got %v", got)
	}
}
