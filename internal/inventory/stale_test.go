package inventory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

func TestComputeStaleSet_DetectsStaleEntries(t *testing.T) {
	previous := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"},
		{Group: "", Kind: "Service", Namespace: "ns", Name: "svc", Version: "v1", Component: "web"},
	}
	current := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v2", Component: "web"},
	}
	stale := ComputeStaleSet(previous, current)
	require.Len(t, stale, 1)
	assert.Equal(t, "svc", stale[0].Name)
}

func TestComputeStaleSet_NoStale(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"},
	}
	stale := ComputeStaleSet(entries, entries)
	assert.Empty(t, stale)
}

func TestComputeStaleSet_EmptyPrevious(t *testing.T) {
	stale := ComputeStaleSet(nil, []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app"},
	})
	assert.Empty(t, stale)
}

func TestComputeStaleSet_VersionAgnosticIdentity(t *testing.T) {
	previous := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"},
	}
	current := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v2", Component: "web"},
	}
	stale := ComputeStaleSet(previous, current)
	assert.Empty(t, stale, "version change should not produce stale entries")
}
