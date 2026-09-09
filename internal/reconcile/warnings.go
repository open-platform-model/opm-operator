package reconcile

import (
	"context"
	"fmt"
	"slices"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// WarningTracker remembers, per object, the set of advisory facts the last
// successful render reported, so the reconciler emits RenderWarning events
// only when an object's set changes rather than on every reconcile
// (enhancement 0019 D18; spec events-emission). It is in-memory: a manager
// restart re-emits the current warnings once, which is the honest outcome
// (the events of the previous process are still on the object).
//
// The set is keyed on the facts (advisoryFacts), not on the worded event
// text: rewording a warning presents an unchanged set as unchanged. Entries
// are keyed by namespaced name and dropped on deletion (Forget), so the map
// is bounded by the number of live objects. The zero value is ready to use;
// one tracker serves one kind.
type WarningTracker struct {
	mu   sync.Mutex
	seen map[types.NamespacedName][]string
}

// Update records facts as the object's current advisory set and reports
// whether the set differs from the previously recorded one
// (order-insensitive, duplicates collapsed). A nil tracker reports every
// non-empty set as a change, so a caller wired without one still surfaces
// warnings.
func (t *WarningTracker) Update(key types.NamespacedName, facts []string) (changed bool) {
	current := distinctSorted(facts)
	if t == nil {
		return len(current) > 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	previous, known := t.seen[key]
	if known && slices.Equal(previous, current) {
		return false
	}
	if t.seen == nil {
		t.seen = make(map[types.NamespacedName][]string)
	}
	if len(current) == 0 {
		// An object that warned before and no longer does: remember that it
		// warns nothing so the next warning is a transition again.
		t.seen[key] = nil
		return known && len(previous) > 0
	}
	t.seen[key] = current
	return true
}

// Forget drops the object's recorded set. Called on deletion.
func (t *WarningTracker) Forget(key types.NamespacedName) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.seen, key)
}

// reportRenderDiagnostics surfaces what a successful render reported beside
// its objects: the warnings as events (emitRenderWarnings) and the
// resolved-versions rows (0019 D18, plain data) in the reconcile log at
// verbosity 1.
func reportRenderDiagnostics(ctx context.Context, tracker *WarningTracker, recorder events.EventRecorder, obj client.Object, result *render.RenderResult) {
	emitRenderWarnings(tracker, recorder, obj, keyOf(obj), result)
	if len(result.ResolvedVersions) > 0 {
		logf.FromContext(ctx).V(1).Info("Resolved module versions", "resolvedVersions", result.ResolvedVersions)
	}
}

// emitRenderWarnings records the render's advisory facts for obj and, when
// the set changed since the previous reconcile, emits one Warning event per
// distinct worded warning with reason RenderWarning and action Render. A
// render with no warnings emits none. The transition check reads the facts
// (advisoryFacts), the event text reads result.Warnings: the two are derived
// from the same rows, so rewording the text alone changes nothing here.
func emitRenderWarnings(tracker *WarningTracker, recorder events.EventRecorder, obj runtime.Object, key types.NamespacedName, result *render.RenderResult) {
	if !tracker.Update(key, advisoryFacts(result)) {
		return
	}
	for _, w := range distinctSorted(result.Warnings) {
		recorder.Eventf(obj, nil, corev1.EventTypeWarning, status.RenderWarningReason, "Render", "%s", w)
	}
}

// advisoryFacts returns one identity key per advisory finding result
// carries, so the tracker compares what the render reported rather than how
// the operator worded it: a resolved-versions row marked Newer keys on the
// path and both versions, an unhandled optional trait on the component and
// the trait. The keys are opaque to callers; only equality matters.
func advisoryFacts(result *render.RenderResult) []string {
	if result == nil {
		return nil
	}
	var facts []string
	for _, r := range result.ResolvedVersions {
		if r.Newer {
			facts = append(facts, fmt.Sprintf("skew %s %s %s", r.Path, r.ModuleVersion, r.PlatformVersion))
		}
	}
	for comp, traits := range result.UnhandledTraits {
		for _, trait := range traits {
			facts = append(facts, fmt.Sprintf("trait %s %s", comp, trait))
		}
	}
	return facts
}

// keyOf returns the namespaced name of obj.
func keyOf(obj client.Object) types.NamespacedName {
	return types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
}

// distinctSorted returns the distinct, non-empty entries of items in sorted
// order.
func distinctSorted(items []string) []string {
	out := make([]string, 0, len(items))
	for _, w := range items {
		if w != "" {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
