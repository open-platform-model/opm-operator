package reconcile

import (
	"context"
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

// WarningTracker remembers, per object, the set of render warnings the last
// successful render reported, so the reconciler emits RenderWarning events
// only when an object's set changes rather than on every reconcile
// (enhancement 0019 D18; spec events-emission). It is in-memory: a manager
// restart re-emits the current warnings once, which is the honest outcome
// (the events of the previous process are still on the object).
//
// Entries are keyed by namespaced name and dropped on deletion (Forget), so
// the map is bounded by the number of live objects. The zero value is ready
// to use; one tracker serves one kind.
type WarningTracker struct {
	mu   sync.Mutex
	seen map[types.NamespacedName][]string
}

// Update records warnings as the object's current set and reports whether
// the set differs from the previously recorded one (order-insensitive,
// duplicates collapsed). A nil tracker reports every non-empty set as a
// change, so a caller wired without one still surfaces warnings.
func (t *WarningTracker) Update(key types.NamespacedName, warnings []string) (changed bool) {
	current := distinctSorted(warnings)
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
	emitRenderWarnings(tracker, recorder, obj, keyOf(obj), result.Warnings)
	if len(result.ResolvedVersions) > 0 {
		logf.FromContext(ctx).V(1).Info("Resolved module versions", "resolvedVersions", result.ResolvedVersions)
	}
}

// emitRenderWarnings records the render's warnings for obj and, when the set
// changed since the previous reconcile, emits one Warning event per distinct
// message with reason RenderWarning and action Render. A render with no
// warnings emits none.
func emitRenderWarnings(tracker *WarningTracker, recorder events.EventRecorder, obj runtime.Object, key types.NamespacedName, warnings []string) {
	if !tracker.Update(key, warnings) {
		return
	}
	for _, w := range distinctSorted(warnings) {
		recorder.Eventf(obj, nil, corev1.EventTypeWarning, status.RenderWarningReason, "Render", "%s", w)
	}
}

// keyOf returns the namespaced name of obj.
func keyOf(obj client.Object) types.NamespacedName {
	return types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
}

// distinctSorted returns the distinct, non-empty messages of warnings in
// sorted order.
func distinctSorted(warnings []string) []string {
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		if w != "" {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
