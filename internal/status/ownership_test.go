package status

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/open-platform-model/library/opm/k8s/ownership"
)

func TestLeftBehindEventType(t *testing.T) {
	ns := LeftObject{Reason: ownership.SkipSafetyExcluded, Message: "ns"}
	tests := []struct {
		name string
		left []LeftObject
		want string
	}{
		{"only safety-excluded kinds", []LeftObject{ns, ns}, corev1.EventTypeNormal},
		{"another instance's object", []LeftObject{ns, {Reason: ownership.SkipOwnerMismatch}}, corev1.EventTypeWarning},
		{"an adopted object", []LeftObject{{Reason: ownership.SkipAdoptedElsewhere}}, corev1.EventTypeWarning},
		{"an unmanaged object", []LeftObject{{Reason: ownership.SkipNotOPMManaged}}, corev1.EventTypeWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LeftBehindEventType(tt.left); got != tt.want {
				t.Fatalf("LeftBehindEventType = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLeftBehindNote(t *testing.T) {
	t.Run("ownership skips come first, messages unchanged", func(t *testing.T) {
		note := LeftBehindNote([]LeftObject{
			{Reason: ownership.SkipSafetyExcluded, Message: "Namespace/team-a is a Namespace, which OPM never deletes; left in place"},
			{Reason: ownership.SkipAdoptedElsewhere, Message: "ConfigMap/team-a/shared is being adopted by module instance u-2, not this one; left in place"},
		})
		want := "Left 2 object(s) in the cluster: " +
			"ConfigMap/team-a/shared is being adopted by module instance u-2, not this one; left in place; " +
			"Namespace/team-a is a Namespace, which OPM never deletes; left in place."
		if note != want {
			t.Fatalf("note =\n%s\nwant\n%s", note, want)
		}
	})

	t.Run("at most ten messages, then a count", func(t *testing.T) {
		left := make([]LeftObject, 0, 13)
		for i := range 13 {
			left = append(left, LeftObject{Reason: ownership.SkipOwnerMismatch, Message: fmt.Sprintf("m%02d", i)})
		}
		note := LeftBehindNote(left)
		if !strings.HasPrefix(note, "Left 13 object(s) in the cluster: m00; ") || !strings.HasSuffix(note, "m09; and 3 more.") {
			t.Fatalf("note = %s", note)
		}
		if strings.Contains(note, "m10") {
			t.Fatalf("note names an eleventh object: %s", note)
		}
	})

	t.Run("long messages stay inside the event note limit", func(t *testing.T) {
		left := make([]LeftObject, 0, 8)
		for i := range 8 {
			left = append(left, LeftObject{
				Reason:  ownership.SkipOwnerMismatch,
				Message: fmt.Sprintf("%d-%s", i, strings.Repeat("x", 300)),
			})
		}
		note := LeftBehindNote(left)
		if len(note) > eventNoteLimit {
			t.Fatalf("note is %d characters, limit %d", len(note), eventNoteLimit)
		}
		if !strings.HasSuffix(note, "; and 5 more.") {
			t.Fatalf("note = ...%s", note[len(note)-40:])
		}
	})

	t.Run("no enhancement reference", func(t *testing.T) {
		if note := LeftBehindNote([]LeftObject{{Reason: ownership.SkipNotOPMManaged, Message: "m"}}); strings.Contains(note, "0012") {
			t.Fatalf("note = %s", note)
		}
	})
}

func TestIdentityChangeUnsettledNote(t *testing.T) {
	note := IdentityChangeUnsettledNote("id-b", "id-a", "id-c")
	for _, want := range []string{"id-a", "id-b", "id-c", "is not finished", "Restore the earlier module path", "Ready"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q: %s", want, note)
		}
	}
	if strings.Contains(note, "0012") {
		t.Errorf("note carries an enhancement reference: %s", note)
	}
}
