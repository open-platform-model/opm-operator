package render

import (
	"fmt"
	"sort"

	"github.com/open-platform-model/library/opm/kernel"
)

// renderWarnings words the render's advisory diagnostic rows as the
// operator's RenderResult.Warnings: one line per resolved-versions row
// marked Newer (catalog skew the Warn policy let through), in the build's
// path order, then one line per unhandled optional trait, components
// sorted, traits in the build's order. The render itself carries no
// message string (library principle: the kernel reports verdicts as data);
// the operator owns this wording, and the reconciler keys its transition
// check on the rows, not on these strings, so rewording here re-emits
// nothing.
//
// Each line names the facts the spec pins: for skew, the OPM-namespace
// path, the version the module requires and the version the platform
// carries; for a trait, the component and the trait.
func renderWarnings(d kernel.RenderDiagnostics) []string {
	var out []string
	for _, r := range d.ResolvedVersions {
		if !r.Newer {
			continue
		}
		out = append(out, fmt.Sprintf(
			"version skew on %q: module requires %s, platform carries %s; rendering against the platform's build",
			r.Path, r.ModuleVersion, r.PlatformVersion))
	}
	comps := make([]string, 0, len(d.UnhandledTraits))
	for c := range d.UnhandledTraits {
		comps = append(comps, c)
	}
	sort.Strings(comps)
	for _, c := range comps {
		for _, fqn := range d.UnhandledTraits[c] {
			out = append(out, fmt.Sprintf(
				"component %q: trait %q is not handled by any matched transformer (values will be ignored)", c, fqn))
		}
	}
	return out
}
