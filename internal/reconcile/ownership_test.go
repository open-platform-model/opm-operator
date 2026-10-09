package reconcile

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// With changed digests every refusal counts. With unchanged digests nothing
// is written over an inventoried object, so only an object outside the
// inventory refuses the reconcile.
func TestRefusedToWrite(t *testing.T) {
	refused := []apply.Judged{
		{Message: "inventoried and terminating", InInventory: true},
		{Message: "another instance's, outside the inventory"},
	}
	if got := refusedToWrite(refused, false); len(got) != 2 {
		t.Errorf("changed digests: %d refusals count, want 2", len(got))
	}
	got := refusedToWrite(refused, true)
	if len(got) != 1 || got[0].InInventory {
		t.Errorf("unchanged digests: refusals %+v, want only the object outside the inventory", got)
	}
	if got := refusedToWrite(refused[:1], true); len(got) != 0 {
		t.Errorf("unchanged digests, an inventoried object alone: %d refusals, want none", len(got))
	}
}

// A failed guard fails the apply when the reconcile would write, and always
// when there is no identity to ask with.
func TestGuardedApplyFailsApply(t *testing.T) {
	read := &apply.GuardReadError{Err: errors.New("boom")}
	tests := []struct {
		name             string
		err              error
		digestsUnchanged bool
		want             bool
	}{
		{"no failure", nil, false, false},
		{"a failed read with changed digests", read, false, true},
		{"a failed read with unchanged digests", read, true, false},
		{"no identity with unchanged digests", apply.ErrNoIdentity, true, true},
	}
	for _, tt := range tests {
		if got := (guardedApply{err: tt.err}).failsApply(tt.digestsUnchanged); got != tt.want {
			t.Errorf("%s: failsApply = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// The count of let-go objects is read from a Ready=True message only: after
// a refused or failed reconcile the next success reports them once more.
func TestAdoptedBefore(t *testing.T) {
	msg := status.ReadyMessage(readySucceeded, 2)
	ready := func(s metav1.ConditionStatus) []metav1.Condition {
		return []metav1.Condition{{Type: status.ReadyCondition, Status: s, Message: msg}}
	}
	if got := adoptedBefore(ready(metav1.ConditionTrue)); got != 2 {
		t.Errorf("Ready=True: %d, want 2", got)
	}
	if got := adoptedBefore(ready(metav1.ConditionFalse)); got != 0 {
		t.Errorf("Ready=False: %d, want 0", got)
	}
	if got := adoptedBefore(nil); got != 0 {
		t.Errorf("no condition: %d, want 0", got)
	}
}
