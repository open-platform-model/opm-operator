package status

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// --- RecordHistory tests (task 3.1) ---

func TestRecordHistory_AppendToEmpty(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}
	entry := releasesv1alpha1.HistoryEntry{Action: "apply"}

	RecordHistory(status, entry)

	require.Len(t, status.History, 1)
	assert.Equal(t, "apply", status.History[0].Action)
}

func TestRecordHistory_TrimAtBoundary(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}
	// Fill to MaxHistoryEntries.
	for i := range MaxHistoryEntries {
		RecordHistory(status, releasesv1alpha1.HistoryEntry{Action: "apply", Sequence: int64(i + 1)})
	}
	require.Len(t, status.History, MaxHistoryEntries)

	// One more should still be exactly MaxHistoryEntries.
	RecordHistory(status, releasesv1alpha1.HistoryEntry{Action: "prune"})

	assert.Len(t, status.History, MaxHistoryEntries)
	assert.Equal(t, "prune", status.History[0].Action, "newest entry should be at index 0")
}

func TestRecordHistory_NewestFirst(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}

	RecordHistory(status, releasesv1alpha1.HistoryEntry{Action: "first"})
	RecordHistory(status, releasesv1alpha1.HistoryEntry{Action: "second"})
	RecordHistory(status, releasesv1alpha1.HistoryEntry{Action: "third"})

	require.Len(t, status.History, 3)
	assert.Equal(t, "third", status.History[0].Action)
	assert.Equal(t, "second", status.History[1].Action)
	assert.Equal(t, "first", status.History[2].Action)
}

// --- Sequence tests (task 3.2) ---

func TestRecordHistory_SequenceMonotonicity(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}

	for i := range 5 {
		RecordHistory(status, releasesv1alpha1.HistoryEntry{Action: "apply"})
		assert.Equal(t, int64(i+1), status.History[0].Sequence,
			"sequence should be %d after %d entries", i+1, i+1)
	}
}

func TestRecordHistory_SequenceMonotonicAfterTrim(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}
	for range MaxHistoryEntries + 3 {
		RecordHistory(status, releasesv1alpha1.HistoryEntry{Action: "apply"})
	}
	// After trimming, the newest entry should have sequence MaxHistoryEntries+3.
	assert.Equal(t, int64(MaxHistoryEntries+3), status.History[0].Sequence,
		"sequence should keep increasing even after trim")
}

func TestNextSequence_Empty(t *testing.T) {
	assert.Equal(t, int64(1), nextSequence(nil))
}

func TestNextSequence_ExistingEntries(t *testing.T) {
	history := []releasesv1alpha1.HistoryEntry{
		{Sequence: 5},
		{Sequence: 3},
		{Sequence: 7},
	}
	assert.Equal(t, int64(8), nextSequence(history))
}

// --- Timestamp tests (task 3.2) ---

func TestNewSuccessEntry_TimestampsPopulated(t *testing.T) {
	entry := NewSuccessEntry("apply", "succeeded", DigestSet{}, 0)
	require.NotNil(t, entry.StartedAt)
	require.NotNil(t, entry.FinishedAt)
	assert.False(t, entry.StartedAt.IsZero())
	assert.False(t, entry.FinishedAt.IsZero())
}

func TestNewFailureEntry_TimestampsPopulated(t *testing.T) {
	entry := NewFailureEntry("apply", "render failed", DigestSet{})
	require.NotNil(t, entry.StartedAt)
	require.NotNil(t, entry.FinishedAt)
	assert.False(t, entry.StartedAt.IsZero())
	assert.False(t, entry.FinishedAt.IsZero())
}

// --- Entry construction tests (task 3.3) ---

func TestNewSuccessEntry_Fields(t *testing.T) {
	digests := DigestSet{
		Source:    "sha256:src",
		Config:    "sha256:cfg",
		Render:    "sha256:rnd",
		Inventory: "sha256:inv",
	}
	entry := NewSuccessEntry("apply", "succeeded", digests, 42)

	assert.Equal(t, "apply", entry.Action)
	assert.Equal(t, "succeeded", entry.Phase)
	assert.Equal(t, "sha256:src", entry.SourceDigest)
	assert.Equal(t, "sha256:cfg", entry.ConfigDigest)
	assert.Equal(t, "sha256:rnd", entry.RenderDigest)
	assert.Equal(t, "sha256:inv", entry.InventoryDigest)
	assert.Equal(t, int64(42), entry.InventoryCount)
	assert.Empty(t, entry.Message, "success entries should not have a message")
}

func TestNewFailureEntry_Fields(t *testing.T) {
	digests := DigestSet{
		Source: "sha256:src",
		Config: "sha256:cfg",
	}
	entry := NewFailureEntry("apply", "render failed: bad template", digests)

	assert.Equal(t, "apply", entry.Action)
	assert.Equal(t, "render failed: bad template", entry.Message)
	assert.Equal(t, "sha256:src", entry.SourceDigest)
	assert.Equal(t, "sha256:cfg", entry.ConfigDigest)
	assert.Empty(t, entry.RenderDigest, "partial digests: render not yet computed")
	assert.Empty(t, entry.InventoryDigest, "partial digests: inventory not provided")
	assert.Empty(t, entry.Phase, "failure entries should not have a phase")
}

func TestNewFailureEntry_EmptyDigests(t *testing.T) {
	entry := NewFailureEntry("apply", "source fetch failed", DigestSet{})

	assert.Equal(t, "source fetch failed", entry.Message)
	assert.Empty(t, entry.SourceDigest)
	assert.Empty(t, entry.ConfigDigest)
	assert.NotNil(t, entry.StartedAt, "timestamps always populated even with empty digests")
}

func TestNewSuccessEntry_PruneAction(t *testing.T) {
	entry := NewSuccessEntry("prune", "succeeded", DigestSet{Source: "sha256:src"}, 3)

	assert.Equal(t, "prune", entry.Action)
	assert.Equal(t, "succeeded", entry.Phase)
	assert.Equal(t, int64(3), entry.InventoryCount)
}

// --- Integration: RecordHistory sets sequence on constructed entries ---

func TestRecordHistory_SetsSequenceOnEntry(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}
	entry := NewSuccessEntry("apply", "succeeded", DigestSet{}, 5)

	// Entry starts with sequence 0 (zero value).
	assert.Equal(t, int64(0), entry.Sequence)

	RecordHistory(status, entry)

	// After recording, the entry in history has sequence set.
	assert.Equal(t, int64(1), status.History[0].Sequence)
}

func TestRecordHistory_PreservesTimestamps(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}
	now := metav1.Now()
	entry := releasesv1alpha1.HistoryEntry{
		Action:    "apply",
		StartedAt: &now,
	}

	RecordHistory(status, entry)

	assert.Equal(t, &now, status.History[0].StartedAt, "RecordHistory should not overwrite timestamps")
}
