package status

import "fmt"

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
	UnreadForbidden Unread = "is forbidden to read them"
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
