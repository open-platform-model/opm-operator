package render

import (
	"fmt"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// RenderResult holds the output of a successful RenderModule call.
// Contains both the rendered resources and their inventory entries, giving the
// caller everything needed for apply + inventory in one call.
type RenderResult struct {
	// Resources is the ordered list of rendered Kubernetes resources.
	Resources []*core.Resource

	// InventoryEntries are the CRD-typed inventory entries built from Resources.
	InventoryEntries []releasesv1alpha1.InventoryEntry

	// Warnings are non-fatal render warnings (e.g., unhandled traits).
	Warnings []string
}

// buildInventoryEntries converts rendered resources to inventory entries.
func buildInventoryEntries(resources []*core.Resource) ([]releasesv1alpha1.InventoryEntry, error) {
	entries := make([]releasesv1alpha1.InventoryEntry, 0, len(resources))
	for _, r := range resources {
		u, err := r.ToUnstructured()
		if err != nil {
			return nil, fmt.Errorf("converting resource %s to unstructured: %w", r, err)
		}
		entries = append(entries, inventory.NewEntryFromResource(u))
	}
	return entries, nil
}
