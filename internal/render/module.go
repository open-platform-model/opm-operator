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

	// Warnings are the render's advisory findings, worded by the operator
	// (renderWarnings) from the diagnostics' rows: effectively-optional unhandled
	// traits and, under the Warn skew policy, catalog version skew. Unresolved
	// demands (undemandable resources, unhandled load-bearing traits) refuse the
	// render instead of landing here (0010:D28). The reconciler emits them as
	// RenderWarning events on transition, keyed on the rows below rather than on
	// these strings.
	Warnings []string

	// UnhandledTraits maps a component to the effectively-optional traits no
	// matched transformer handles, as the build reported it. Plain data: the
	// facts behind the trait warnings, the reconciler's transition key.
	UnhandledTraits map[string][]string

	// ResolvedVersions are the per-path version rows the build reports
	// (0019:D18): for every OPM-namespace path the instance module requires,
	// the build it asked for and the build the platform carries. Plain data;
	// the reconciler logs them at debug level, and a row marked Newer is the
	// fact behind a skew warning.
	ResolvedVersions []kernel.ResolvedVersion

	// RequiredContracts is every contract FQN the instance's components
	// declare, sorted and deduplicated (0015:D3, D16): the
	// instance's demand, in the keyspace TransformerRegistration.spec.provides
	// carries. The reconciler persists it on status.requiredContracts, where
	// the claim reconciler's removal guard intersects it with a claim's
	// provides to count that claim's dependents.
	//
	// Read off the synthesized instance, never off the platform, so it is a
	// property of the instance alone and does not move when the platform does.
	RequiredContracts []string

	// PlatformIdentity is the identity of the generated platform package this
	// render built against, in its string form (0015:D13, D17):
	// the Platform CR generation plus a digest of the active claims' catalog
	// coordinates. A render holds its package under a lease for its whole
	// duration, so this is the exact registry state the render consumed, even
	// when a newer package was generated while it ran.
	PlatformIdentity string
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
