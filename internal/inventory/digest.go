package inventory

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// ComputeDigest returns a deterministic SHA-256 digest of the inventory entries.
// Entries are sorted by Group, Kind, Namespace, Name, Component, Version before
// hashing. Returns a string in the format "sha256:<hex>".
func ComputeDigest(entries []releasesv1alpha1.InventoryEntry) string {
	sorted := make([]releasesv1alpha1.InventoryEntry, len(entries))
	copy(sorted, entries)
	if len(sorted) == 0 {
		sum := sha256.Sum256(nil)
		return fmt.Sprintf("sha256:%x", sum)
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Group != sorted[j].Group {
			return sorted[i].Group < sorted[j].Group
		}
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind < sorted[j].Kind
		}
		if sorted[i].Namespace != sorted[j].Namespace {
			return sorted[i].Namespace < sorted[j].Namespace
		}
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		if sorted[i].Component != sorted[j].Component {
			return sorted[i].Component < sorted[j].Component
		}
		return sorted[i].Version < sorted[j].Version
	})

	b, err := json.Marshal(sorted)
	if err != nil {
		b = fmt.Appendf(nil, "%v", sorted)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", sum)
}
