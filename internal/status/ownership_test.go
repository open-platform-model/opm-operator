package status

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

// refusalMessage is a refusal as the library words it: the tests take the
// text from the verdict, never from a copy of it.
func refusalMessage(t *testing.T, name string) string {
	t.Helper()
	live := &unstructured.Unstructured{}
	live.SetAPIVersion("v1")
	live.SetKind("ConfigMap")
	live.SetNamespace("team-a")
	live.SetName(name)
	v := ownership.CanApply(ownership.ApplyInput{
		Object:       ownership.Object{Kind: "ConfigMap", Namespace: "team-a", Name: name},
		Live:         live,
		InstanceUUID: "u-1",
	})
	if v.Refuse != ownership.RefuseForeignObject || v.Message == "" {
		t.Fatalf("verdict = %+v, want a foreign-object refusal", v)
	}
	return v.Message
}

func TestApplyRefusedNote(t *testing.T) {
	t.Run("count, nothing applied, the library's messages unchanged", func(t *testing.T) {
		a, b := refusalMessage(t, "settings"), refusalMessage(t, "other")
		note := ApplyRefusedNote([]string{a, b})
		want := "Refused to apply over 2 object(s), nothing was applied: " + a + "; " + b + "."
		if note != want {
			t.Fatalf("note =\n%s\nwant\n%s", note, want)
		}
		if !strings.Contains(note, "ConfigMap/team-a/settings") || !strings.Contains(note, "=u-1") {
			t.Fatalf("note does not name the object and the identity to set: %s", note)
		}
	})

	t.Run("at most ten messages, then a count", func(t *testing.T) {
		messages := make([]string, 0, 13)
		for i := range 13 {
			messages = append(messages, fmt.Sprintf("m%02d", i))
		}
		note := ApplyRefusedNote(messages)
		if !strings.HasSuffix(note, "m09; and 3 more.") || strings.Contains(note, "m10") {
			t.Fatalf("note = %s", note)
		}
	})

	t.Run("never past the event note limit", func(t *testing.T) {
		long := strings.Repeat("x", 400)
		for _, n := range []int{1, 2, 3, 12} {
			messages := make([]string, n)
			for i := range messages {
				messages[i] = long
			}
			for _, note := range []string{ApplyRefusedNote(messages), AdoptedElsewhereNote(messages)} {
				if len(note) > eventNoteLimit {
					t.Fatalf("%d messages: note is %d characters, limit %d", n, len(note), eventNoteLimit)
				}
			}
		}
		note := ApplyRefusedNote([]string{strings.Repeat("x", 2000)})
		if !strings.HasSuffix(note, "1 objects, messages too long to list.") {
			t.Fatalf("note = %s", note)
		}
	})

	t.Run("no enhancement reference", func(t *testing.T) {
		note := ApplyRefusedNote([]string{refusalMessage(t, "settings")}) + AdoptedElsewhereNote([]string{"m"}) +
			ReadyMessage("Reconciliation succeeded", 1)
		if regexp.MustCompile(`\d{4}:D\d+`).MatchString(note) {
			t.Fatalf("note carries an enhancement reference: %s", note)
		}
	})
}

func TestAdoptedElsewhereNote(t *testing.T) {
	note := AdoptedElsewhereNote([]string{"m1", "m2"})
	want := "2 rendered object(s) are adopted by another instance and are not applied: m1; m2."
	if note != want {
		t.Fatalf("note =\n%s\nwant\n%s", note, want)
	}
}

func TestReadyMessageCountsAdoptedObjects(t *testing.T) {
	tests := []struct {
		base    string
		adopted int
		want    string
	}{
		{"Reconciliation succeeded", 0, "Reconciliation succeeded"},
		{"Reconciliation succeeded", 2,
			"Reconciliation succeeded. 2 rendered object(s) are adopted by another instance and are not applied."},
		{"No changes detected.", 1,
			"No changes detected. 1 rendered object(s) are adopted by another instance and are not applied."},
	}
	for _, tt := range tests {
		got := ReadyMessage(tt.base, tt.adopted)
		if got != tt.want {
			t.Fatalf("ReadyMessage(%q, %d) = %q, want %q", tt.base, tt.adopted, got, tt.want)
		}
		// The count is read back from the message alone: it is what tells
		// the next reconcile whether the number changed.
		if n := AdoptedElsewhereCount(got); n != tt.adopted {
			t.Fatalf("AdoptedElsewhereCount(%q) = %d, want %d", got, n, tt.adopted)
		}
	}
	for _, other := range []string{"", "Reconciliation in progress", "12 rendered object(s) are adopted by another instance and are not applied. More."} {
		if n := AdoptedElsewhereCount(other); n != 0 {
			t.Fatalf("AdoptedElsewhereCount(%q) = %d, want 0", other, n)
		}
	}
}
