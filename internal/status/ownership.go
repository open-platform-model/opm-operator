package status

import (
	"fmt"
	"regexp"
	"strconv"
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

	// ApplyRefusedReason: Ready=False, not Stalled, the apply verdict refused
	// an object of the render, so nothing was applied or pruned. Retried on
	// the bounded backoff. Also the reason of the event that reports it.
	ApplyRefusedReason = "ApplyRefused"

	// AdoptedElsewhereReason is the reason of the event that names the
	// rendered objects an instance does not apply because their adopt
	// annotation names another instance. It is no condition reason: the
	// instance stays Ready, and the Ready message counts the objects.
	AdoptedElsewhereReason = "AdoptedElsewhere"

	// leftBehindMaxMessages is the most objects a LeftBehind note names. The
	// ApplyRefused and AdoptedElsewhere notes have the same limit.
	leftBehindMaxMessages = 10

	// adoptedElsewhereClause is the sentence a Ready message ends with when
	// rendered objects are adopted by another instance. ReadyMessage writes
	// it and AdoptedElsewhereCount reads the number back.
	adoptedElsewhereClause = "%d rendered object(s) are adopted by another instance and are not applied."
)

var adoptedElsewhereCount = regexp.MustCompile(`(\d+) rendered object\(s\) are adopted by another instance and are not applied\.$`)

// ApplyRefusedNote is the message of the ApplyRefused reason, on the Ready
// condition and on the event: how many objects the apply verdict refused,
// that nothing was applied, and for each object the library's message,
// unchanged. It carries at most ten messages, and fewer when they would take
// the note past the event note limit; the rest is a count.
func ApplyRefusedNote(messages []string) string {
	return listMessages(fmt.Sprintf("Refused to apply over %d object(s), nothing was applied: ", len(messages)), messages)
}

// AdoptedElsewhereNote is the note of an AdoptedElsewhere event: how many
// rendered objects are adopted by another instance and, for each, the
// library's message, unchanged, within the limits of ApplyRefusedNote.
func AdoptedElsewhereNote(messages []string) string {
	return listMessages(fmt.Sprintf("%d rendered object(s) are adopted by another instance and are not applied: ",
		len(messages)), messages)
}

// ReadyMessage is the message of Ready=True after a reconcile that rendered:
// base, and when adopted is not zero the number of rendered objects that are
// adopted by another instance and not applied.
func ReadyMessage(base string, adopted int) string {
	if adopted <= 0 {
		return base
	}
	return strings.TrimRight(base, ". ") + ". " + fmt.Sprintf(adoptedElsewhereClause, adopted)
}

// AdoptedElsewhereCount returns the number of adopted objects a message
// written by ReadyMessage states, zero when it states none.
func AdoptedElsewhereCount(readyMessage string) int {
	m := adoptedElsewhereCount.FindStringSubmatch(readyMessage)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

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

	messages := make([]string, 0, len(ordered))
	for _, l := range ordered {
		messages = append(messages, l.Message)
	}
	return listMessages(fmt.Sprintf("Left %d object(s) in the cluster: ", len(ordered)), messages)
}

// listMessages returns head followed by the messages, joined and unchanged.
// It carries at most leftBehindMaxMessages of them, and fewer when they would
// take the text past the event note limit; the rest is a count.
func listMessages(head string, all []string) string {
	// Room for the messages: the limit less the head and the longest
	// possible tail.
	budget := eventNoteLimit - len(head) - len(fmt.Sprintf("; and %d more.", len(all)))

	var messages []string
	used := 0
	for _, m := range all {
		if len(messages) == leftBehindMaxMessages {
			break
		}
		cost := len(m)
		if len(messages) > 0 {
			cost += len("; ")
		}
		if used+cost > budget {
			break
		}
		messages = append(messages, m)
		used += cost
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString(strings.Join(messages, "; "))
	switch rest := len(all) - len(messages); {
	case rest == 0:
		b.WriteString(".")
	case len(messages) == 0:
		fmt.Fprintf(&b, "%d objects, messages too long to list.", rest)
	default:
		fmt.Fprintf(&b, "; and %d more.", rest)
	}
	return b.String()
}
