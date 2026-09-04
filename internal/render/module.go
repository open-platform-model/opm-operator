package render

import (
	"fmt"

	"github.com/open-platform-model/library/opm/kernel"

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

	// Warnings are the render build's advisory, human-readable messages:
	// effectively-optional unhandled traits and, under the Warn skew policy,
	// catalog version skew. Unresolved demands (undemandable resources,
	// unhandled load-bearing traits) refuse the render instead of landing
	// here (0010 D28). The reconciler emits them as RenderWarning events on
	// transition.
	Warnings []string

	// ResolvedVersions are the per-path version rows the build reports
	// (0019 D18): for every OPM-namespace path the instance module requires,
	// the build it asked for and the build the platform carries. Plain data;
	// the reconciler logs them at debug level.
	ResolvedVersions []kernel.ResolvedVersion
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
