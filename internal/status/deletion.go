package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// DeletionUnconfirmedReason is the reason of the Warning event a deletion
// cleanup emits when it removes the cleanup finalizer without having read
// every object: the objects may still exist.
const DeletionUnconfirmedReason = "DeletionUnconfirmed"

// Unread says why a deletion cleanup could not read objects.
type Unread string

// The causes of an unread object.
const (
	// UnreadIdentityMissing: the ServiceAccount does not exist.
	UnreadIdentityMissing Unread = "is missing"
	// UnreadIdentityFailed: the ServiceAccount could not be impersonated.
	UnreadIdentityFailed Unread = "cannot be impersonated"
	// UnreadForbidden: the reads were refused as Forbidden.
	UnreadForbidden Unread = "is not allowed to read them"
)

// unreadCause words who could not read, and why. identity is the namespaced
// name of the ServiceAccount, or empty when the controller reads as itself.
func unreadCause(identity string, why Unread) string {
	if identity == "" {
		return "the controller " + string(why)
	}
	return fmt.Sprintf("ServiceAccount %q %s", identity, why)
}

// DeletionUnconfirmedNote words the event of a deletion that was released
// after every delete was sent, without a confirmation that count objects are
// gone.
func DeletionUnconfirmedNote(count int, identity string, why Unread) string {
	return fmt.Sprintf("Removed the cleanup finalizer without confirming that %d object(s) are gone: %s. "+
		"Every delete was sent before; the objects may still exist. Check the namespace for leftovers.",
		count, unreadCause(identity, why))
}

// ClaimsUnreadNote words the event of a deletion whose inventory held only
// PersistentVolumeClaims the data policy keeps, released without a read.
func ClaimsUnreadNote(count int, identity string, why Unread) string {
	return fmt.Sprintf("Removed the cleanup finalizer without reading %d PersistentVolumeClaim(s) "+
		"that spec.dataPolicy keeps: %s. The claims were left in place and may still exist.",
		count, unreadCause(identity, why))
}

// The reasons of the Ready condition of a ModuleInstance or a ModulePackage
// whose deletion cleanup sent every delete and waits for the deleted objects
// to be gone. They are set by MarkDeletionInProgress and MarkDeletionBlocked
// only, and only on an object that is being deleted. A deletion cleanup reads
// them back as the record that every delete was sent.
const (
	// DeletionInProgressReason: Ready=False, Reconciling=True. The deleted
	// objects are still terminating.
	DeletionInProgressReason = "DeletionInProgress"
	// DeletionBlockedReason: Ready=False, Stalled=True. A deleted object has
	// not gone for longer than the blocked threshold.
	DeletionBlockedReason = "DeletionBlocked"
)

// The bounds of a deletion wait message. Finalizer names are written by
// whoever may write the object, so their number in a message is bounded.
const (
	waitingMaxObjects    = 10
	waitingMaxFinalizers = 3
)

// WaitingObject is one object a deletion cleanup deleted that still exists.
type WaitingObject struct {
	Kind, Namespace, Name string
	Finalizers            []string
}

// String names the object and what holds it. Finalizers other than the
// garbage collector's foregroundDeletion come first: every repeated
// Foreground delete sets foregroundDeletion again, so it is rarely the cause.
func (o WaitingObject) String() string {
	name := o.Kind + "/" + o.Name
	if o.Namespace != "" {
		name = o.Kind + "/" + o.Namespace + "/" + o.Name
	}
	var others []string
	foreground := false
	for _, f := range o.Finalizers {
		if f == metav1.FinalizerDeleteDependents {
			foreground = true
			continue
		}
		others = append(others, f)
	}
	switch {
	case len(others) == 0 && foreground:
		return name + " (waits for its dependents to be deleted)"
	case len(others) == 0:
		return name + " (terminating, no finalizer)"
	}
	if foreground {
		others = append(others, metav1.FinalizerDeleteDependents)
	}
	named := others
	if len(named) > waitingMaxFinalizers {
		named = named[:waitingMaxFinalizers]
	}
	text := name + " (finalizers: " + strings.Join(named, ", ")
	if rest := len(others) - len(named); rest > 0 {
		text += fmt.Sprintf(" and %d more", rest)
	}
	return text + ")"
}

// listWaiting names at most waitingMaxObjects objects and counts the rest.
func listWaiting(objects []WaitingObject) string {
	named := objects
	if len(named) > waitingMaxObjects {
		named = named[:waitingMaxObjects]
	}
	parts := make([]string, len(named))
	for i, o := range named {
		parts[i] = o.String()
	}
	text := strings.Join(parts, "; ")
	if rest := len(objects) - len(named); rest > 0 {
		text += fmt.Sprintf("; and %d more", rest)
	}
	return text
}

// DeletionInProgressNote words the Ready message of a deletion that waits.
func DeletionInProgressNote(objects []WaitingObject) string {
	return fmt.Sprintf("Every delete was sent; waiting for %d object(s) to be gone: %s.",
		len(objects), listWaiting(objects))
}

// DeletionBlockedNote words the Ready message of a deletion whose deleted
// objects have not gone for longer than after, with the ways out.
func DeletionBlockedNote(objects []WaitingObject, after time.Duration) string {
	return fmt.Sprintf("%d deleted object(s) are still terminating after more than %s: %s. "+
		"Ways out: (1) remove what holds each object: delete the dependent that cannot stop, "+
		"or fix or remove the controller that owns the named finalizer; "+
		"(2) set spec.prune=false on this object to remove its finalizer and leave the objects as they are. "+
		"The annotation %s does not release this wait.",
		len(objects), after, listWaiting(objects), releasesv1alpha1.AnnotationForceDeleteOrphan)
}

// DeletionCheckFailedNote words the Ready message of a waiting deletion
// whose recheck could not be done. what names the failed check and its cause.
func DeletionCheckFailedNote(what string) string {
	return fmt.Sprintf("Every delete was sent; the deleted objects could not be checked: %s. The check is retried.", what)
}

// DeletionCheckBlockedNote words the Ready message of a deletion that has
// been waiting for longer than after and whose recheck cannot be done, with
// the ways out.
func DeletionCheckBlockedNote(what string, after time.Duration) string {
	return fmt.Sprintf("Every delete was sent more than %s ago and the deleted objects could not be checked: %s. "+
		"The check is retried. Ways out: (1) restore what the check needs: the API server, the rights of the "+
		"ServiceAccount, or the controller's right to impersonate it; "+
		"(2) set spec.prune=false on this object to remove its finalizer and leave the objects as they are. "+
		"The annotation %s does not release this wait.",
		after, what, releasesv1alpha1.AnnotationForceDeleteOrphan)
}

// EventNote cuts a condition message to the length an event note may have.
func EventNote(message string) string {
	if len(message) <= eventNoteLimit {
		return message
	}
	return message[:eventNoteLimit-3] + "..."
}

// MarkDeletionInProgress records that a deleting object waits for its
// deleted objects: Ready=False and Reconciling=True with reason
// DeletionInProgress, and Stalled removed. It does nothing on an object that
// is not being deleted, so the reason never appears on a live object.
func MarkDeletionInProgress(obj conditions.Setter, message string) {
	if obj.GetDeletionTimestamp().IsZero() {
		return
	}
	conditions.MarkReconciling(obj, DeletionInProgressReason, "%s", message)
	conditions.MarkFalse(obj, ReadyCondition, DeletionInProgressReason, "%s", message)
}

// MarkDeletionBlocked records that a deleting object's deleted objects do not
// go: Ready=False and Stalled=True with reason DeletionBlocked, and
// Reconciling removed. It does nothing on an object that is not being
// deleted.
func MarkDeletionBlocked(obj conditions.Setter, message string) {
	if obj.GetDeletionTimestamp().IsZero() {
		return
	}
	MarkStalled(obj, DeletionBlockedReason, "%s", message)
}
