package inventory

import releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"

// ComputeStaleSet returns entries present in previous but absent from current.
// Uses IdentityEqual for comparison, meaning Version changes do not produce
// stale entries.
func ComputeStaleSet(previous, current []releasesv1alpha1.InventoryEntry) []releasesv1alpha1.InventoryEntry {
	if len(previous) == 0 {
		return []releasesv1alpha1.InventoryEntry{}
	}

	stale := make([]releasesv1alpha1.InventoryEntry, 0)
	for _, prev := range previous {
		found := false
		for _, cur := range current {
			if IdentityEqual(prev, cur) {
				found = true
				break
			}
		}
		if !found {
			stale = append(stale, prev)
		}
	}

	return stale
}
