package reconcile

import (
	"strings"
	"testing"

	"k8s.io/client-go/tools/events"

	"github.com/open-platform-model/library/opm/k8s/ownership"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
)

// One LeftBehind event per run: Normal when only kinds OPM never deletes were
// left, Warning with the ownership skips first otherwise, none when nothing
// was left.
func TestReportLeftBehind(t *testing.T) {
	ns := apply.LeftBehind{
		Entry:   releasesv1alpha1.InventoryEntry{Kind: "Namespace", Name: "team-a"},
		Reason:  ownership.SkipSafetyExcluded,
		Message: "Namespace/team-a is a Namespace, which OPM never deletes; left in place",
	}
	adopted := apply.LeftBehind{
		Entry:   releasesv1alpha1.InventoryEntry{Kind: "ConfigMap", Namespace: "team-a", Name: "shared"},
		Reason:  ownership.SkipAdoptedElsewhere,
		Message: "ConfigMap/team-a/shared is being adopted by module instance u-2, not this one; left in place",
	}
	tests := []struct {
		name string
		left []apply.LeftBehind
		want string // prefix of the one event; empty for none
	}{
		{"nothing left", nil, ""},
		{"only a Namespace", []apply.LeftBehind{ns}, "Normal LeftBehind Left 1 object(s) in the cluster: Namespace/team-a"},
		{
			"a Namespace and an adopted object", []apply.LeftBehind{ns, adopted},
			"Warning LeftBehind Left 2 object(s) in the cluster: ConfigMap/team-a/shared is being adopted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := events.NewFakeRecorder(4)
			reportLeftBehind(rec, deletingMR("", ""), "Delete", tt.left)
			got := drainEvents(rec)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("events = %v, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.HasPrefix(got[0], tt.want) {
				t.Fatalf("events = %v, want one with prefix %q", got, tt.want)
			}
			if !strings.Contains(got[0], ns.Message) {
				t.Fatalf("event %q lacks the Namespace's message", got[0])
			}
		})
	}
}
