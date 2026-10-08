package reconcile

import (
	"reflect"
	"testing"
)

func TestPlanIdentities(t *testing.T) {
	const a, b, c = "id-a", "id-b", "id-c"
	tests := []struct {
		name                         string
		recorded, previous, rendered string
		legacy                       bool
		want                         identityPlan
	}{
		{
			name: "nothing recorded: the render's identity alone", rendered: a,
			want: identityPlan{InstanceUUID: a, Changed: true, Prune: []string{a}},
		},
		{
			name: "nothing recorded, legacy fallback: then no identity", rendered: a, legacy: true,
			want: identityPlan{InstanceUUID: a, Changed: true, Prune: []string{a, ""}},
		},
		{
			name: "the legacy fallback changes nothing once an identity is recorded", recorded: a, rendered: b, legacy: true,
			want: identityPlan{InstanceUUID: b, PreviousInstanceUUID: a, Changed: true, Prune: []string{b, a}},
		},
		{
			name: "unchanged", recorded: a, rendered: a,
			want: identityPlan{InstanceUUID: a, Prune: []string{a}},
		},
		{
			name: "unchanged while a change is pending", recorded: b, previous: a, rendered: b,
			want: identityPlan{InstanceUUID: b, PreviousInstanceUUID: a, Prune: []string{b, a}},
		},
		{
			name: "A to B", recorded: a, rendered: b,
			want: identityPlan{InstanceUUID: b, PreviousInstanceUUID: a, Changed: true, Prune: []string{b, a}},
		},
		{
			name: "back to A", recorded: b, previous: a, rendered: a,
			want: identityPlan{InstanceUUID: a, PreviousInstanceUUID: b, Changed: true, Prune: []string{a, b}},
		},
		{
			name: "a third identity", recorded: b, previous: a, rendered: c,
			want: identityPlan{InstanceUUID: b, PreviousInstanceUUID: a, Refused: true, Prune: []string{b, a}},
		},
		{
			name: "nothing recorded and an empty render identity",
			want: identityPlan{},
		},
		{
			name: "recorded and an empty render identity", recorded: a,
			want: identityPlan{InstanceUUID: a, Prune: []string{a}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planIdentities(tt.recorded, tt.previous, tt.rendered, tt.legacy)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("planIdentities(%q, %q, %q) = %+v, want %+v", tt.recorded, tt.previous, tt.rendered, got, tt.want)
			}
		})
	}
}
