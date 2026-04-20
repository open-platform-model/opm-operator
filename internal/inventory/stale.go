package inventory

import releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"

// ComputeStaleSet returns entries present in previous but absent from current.
// Uses K8sIdentityEqual for comparison: matches on Group, Kind, Namespace, and
// Name only. Version is excluded so API migrations (e.g. v1beta1 → v1) do not
// produce false stale entries, and Component is excluded so CUE refactors that
// move a resource between components do not destroy the live object that SSA
// apply patches in place.
func ComputeStaleSet(previous, current []releasesv1alpha1.InventoryEntry) []releasesv1alpha1.InventoryEntry {
	if len(previous) == 0 {
		return []releasesv1alpha1.InventoryEntry{}
	}

	stale := make([]releasesv1alpha1.InventoryEntry, 0)
	for _, prev := range previous {
		found := false
		for _, cur := range current {
			if K8sIdentityEqual(prev, cur) {
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
