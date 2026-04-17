package status

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

const (
	// MaxHistoryEntries is the maximum number of history entries retained.
	// Matches docs/design/module-release-reconcile-loop.md retention policy
	// (design decision 1).
	MaxHistoryEntries = 10
)

// NewSuccessEntry creates a HistoryEntry for a successful reconcile action.
// Populates timestamps via metav1.Now() automatically (design decision 2: typed helpers).
// Digest fields are populated from the DigestSet (change 6).
func NewSuccessEntry(
	action string,
	phase string,
	digests DigestSet,
	inventoryCount int64,
) releasesv1alpha1.HistoryEntry {
	now := metav1.Now()
	return releasesv1alpha1.HistoryEntry{
		Action:          action,
		Phase:           phase,
		StartedAt:       &now,
		FinishedAt:      &now,
		SourceDigest:    digests.Source,
		ConfigDigest:    digests.Config,
		RenderDigest:    digests.Render,
		InventoryDigest: digests.Inventory,
		InventoryCount:  inventoryCount,
	}
}

// NewFailureEntry creates a HistoryEntry for a failed reconcile attempt.
// Populates timestamps via metav1.Now(). Digests may be partially filled
// depending on which phase failed.
func NewFailureEntry(
	action string,
	message string,
	digests DigestSet,
) releasesv1alpha1.HistoryEntry {
	now := metav1.Now()
	return releasesv1alpha1.HistoryEntry{
		Action:          action,
		StartedAt:       &now,
		FinishedAt:      &now,
		SourceDigest:    digests.Source,
		ConfigDigest:    digests.Config,
		RenderDigest:    digests.Render,
		InventoryDigest: digests.Inventory,
		Message:         message,
	}
}

// RecordHistory prepends entry to status.History and trims to MaxHistoryEntries.
// The entry's Sequence is set to nextSequence(status.History) before prepending.
// Newest entry is at index 0 after prepend.
//
// Does not record periodic no-ops (caller responsibility — design doc explicitly
// excludes recording every periodic no-op).
func RecordHistory(
	status *releasesv1alpha1.ModuleReleaseStatus,
	entry releasesv1alpha1.HistoryEntry,
) {
	recordHistoryEntry(&status.History, entry)
}

// RecordReleaseHistory is the Release equivalent of RecordHistory.
func RecordReleaseHistory(
	status *releasesv1alpha1.ReleaseStatus,
	entry releasesv1alpha1.HistoryEntry,
) {
	recordHistoryEntry(&status.History, entry)
}

func recordHistoryEntry(history *[]releasesv1alpha1.HistoryEntry, entry releasesv1alpha1.HistoryEntry) {
	entry.Sequence = nextSequence(*history)
	*history = append([]releasesv1alpha1.HistoryEntry{entry}, *history...)
	if len(*history) > MaxHistoryEntries {
		*history = (*history)[:MaxHistoryEntries]
	}
}

// nextSequence returns max(existing sequences) + 1 for monotonic ordering
// (design decision 3). Returns 1 if history is empty.
func nextSequence(history []releasesv1alpha1.HistoryEntry) int64 {
	if len(history) == 0 {
		return 1
	}
	var max int64
	for _, e := range history {
		if e.Sequence > max {
			max = e.Sequence
		}
	}
	return max + 1
}
