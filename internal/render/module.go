package render

import (
	"github.com/open-platform-model/library/opm/k8s/object"
	"github.com/open-platform-model/library/opm/kernel"
)

// RenderResult holds the output of a successful RenderModule call: the
// rendered resources, still backed by their CUE values, and the render's
// plain data. It carries no inventory entries and no digest: the reconciler
// exports the resources once and builds both from that export.
type RenderResult struct {
	// Resources is the ordered list of rendered Kubernetes resources.
	Resources []*object.Resource

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

	// RequiredContracts is the kernel's contract demand for the instance
	// (0013:D24): every #resources and #traits key of every component,
	// sorted and deduplicated, in the keyspace
	// TransformerRegistration.spec.provides carries. The render build
	// computes it from the instance alone and does not narrow it by the
	// platform, so it does not move when the platform does. Never nil. The
	// reconciler persists it on status.requiredContracts, where the claim
	// reconciler's removal guard intersects it with a claim's provides to
	// count that claim's dependents (0015:D3/D16).
	RequiredContracts []string

	// ModuleVersion is the version the rendered instance's source module
	// declares in metadata.version, as the module spells it (bare SemVer),
	// read off the instance the same way on both render paths. Empty when it
	// cannot be read as a concrete string; that never fails the render. The
	// reconciler records it on status.lastAppliedVersion.
	ModuleVersion string

	// PlatformIdentity is the identity of the generated platform package this
	// render built against, in its string form (0015:D13, D17):
	// the Platform CR generation plus a digest of the active claims' catalog
	// coordinates. A render holds its package under a lease for its whole
	// duration, so this is the exact registry state the render consumed, even
	// when a newer package was generated while it ran.
	PlatformIdentity string

	// SkewPolicy is the catalog skew policy of the platform record this
	// render leased, spelled as Platform.spec.skewPolicy spells it ("Warn" or
	// "Refuse"). Together with PlatformIdentity it names the platform the
	// render actually used; the reconciler records both in the render input
	// key.
	SkewPolicy string
}
