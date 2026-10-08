package status

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/open-platform-model/library/opm/k8s/ownership"
)

const (
	// IdentityChangeUnsettledReason: Ready=False, Stalled=True, the render
	// carries a third instance identity while an earlier change of the
	// identity is not settled. Nothing is applied or pruned, and both
	// recorded identities stay. Also the reason of the event that reports it.
	IdentityChangeUnsettledReason = "IdentityChangeUnsettled"

	// LeftBehindReason is the reason of the event that names the objects a
	// prune or a deletion cleanup left in the cluster because the delete
	// verdict skipped them.
	LeftBehindReason = "LeftBehind"

	// leftBehindMaxMessages is the most objects a LeftBehind note names.
	leftBehindMaxMessages = 10
)

// IdentityChangeUnsettledNote is the message of the IdentityChangeUnsettled
// reason, on the Ready condition and on the event: which change is not
// finished, which identity the render now carries, and the way out.
func IdentityChangeUnsettledNote(current, previous, rendered string) string {
	return fmt.Sprintf("An earlier change of the instance identity, from %s to %s, is not finished, "+
		"and the module path changed again: the render now carries identity %s. Nothing was applied or pruned. "+
		"Restore the earlier module path, wait until the object is Ready, then change the module path again.",
		previous, current, rendered)
}

// LeftObject is one object a prune left in the cluster: the reason of the
// delete verdict that skipped it and the message the library words for it.
type LeftObject struct {
	Reason  ownership.SkipReason
	Message string
}

// LeftBehindEventType is the type of a LeftBehind event: Normal when every
// object left is of a kind OPM never deletes, Warning when at least one was
// left for an ownership reason.
func LeftBehindEventType(left []LeftObject) string {
	for _, l := range left {
		if l.Reason != ownership.SkipSafetyExcluded {
			return corev1.EventTypeWarning
		}
	}
	return corev1.EventTypeNormal
}

// LeftBehindNote is the note of a LeftBehind event: how many objects were
// left in the cluster and, for each, the library's message, unchanged. The
// objects left for an ownership reason come first. It carries at most ten
// messages, and fewer when they would take the note past the event note
// limit; the rest is a count.
func LeftBehindNote(left []LeftObject) string {
	ordered := make([]LeftObject, 0, len(left))
	for _, l := range left {
		if l.Reason != ownership.SkipSafetyExcluded {
			ordered = append(ordered, l)
		}
	}
	for _, l := range left {
		if l.Reason == ownership.SkipSafetyExcluded {
			ordered = append(ordered, l)
		}
	}

	head := fmt.Sprintf("Left %d object(s) in the cluster: ", len(ordered))
	// Room for the messages: the limit less the head and the longest
	// possible tail.
	budget := eventNoteLimit - len(head) - len(fmt.Sprintf("; and %d more.", len(ordered)))

	var messages []string
	used := 0
	for _, l := range ordered {
		if len(messages) == leftBehindMaxMessages {
			break
		}
		cost := len(l.Message)
		if len(messages) > 0 {
			cost += len("; ")
		}
		if used+cost > budget {
			break
		}
		messages = append(messages, l.Message)
		used += cost
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString(strings.Join(messages, "; "))
	switch rest := len(ordered) - len(messages); {
	case rest == 0:
		b.WriteString(".")
	case len(messages) == 0:
		fmt.Fprintf(&b, "%d objects, messages too long to list.", rest)
	default:
		fmt.Fprintf(&b, "; and %d more.", rest)
	}
	return b.String()
}
