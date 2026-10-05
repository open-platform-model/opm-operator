package reconcile

// appliedVersion is the value a successful apply records on
// status.lastAppliedVersion: the version this attempt's render reported, or
// "" when it reported none, so a stale version never survives an apply of a
// module whose version could not be read.
func appliedVersion(rendered *string) string {
	if rendered == nil {
		return ""
	}
	return *rendered
}

// recordNoOpVersion is the NoOp commit's write of status.lastAppliedVersion.
// A NoOp means every digest equals the last apply's, and the source digest
// pins the module version, so the version this attempt rendered is the one
// last applied, whoever applied it. Writing it whatever the field held fills
// the field on an object applied before it existed and corrects it after an
// ownership handback, where the cli moved the digests without touching it.
//
// rendered is nil when the attempt did not render; the field is then left as
// it is. A reconcile that skips the render must keep it nil.
func recordNoOpVersion(field, rendered *string) {
	if rendered != nil {
		*field = *rendered
	}
}
