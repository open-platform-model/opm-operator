package render

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/open-platform-model/library/opm/kernel"
)

// The operator words the render's advisory rows itself: one line per
// resolved-versions row marked Newer and one per unhandled optional trait,
// each naming the facts the spec pins (path and both versions; component
// and trait). Rows the policy did not flag produce nothing.
func TestRenderWarnings_WordsSkewAndUnhandledTraits(t *testing.T) {
	diag := kernel.RenderDiagnostics{
		ResolvedVersions: []kernel.ResolvedVersion{
			{Path: "opmodel.dev/core@v2", ModuleVersion: "v2.0.0", PlatformVersion: "v2.0.0"},
			{Path: "opmodel.dev/catalogs/opm@v4", ModuleVersion: "v4.1.0", PlatformVersion: "v4.0.1", Newer: true},
		},
		UnhandledTraits: map[string][]string{
			"web": {"opmodel.dev/catalogs/opm/traits/expose@v4"},
		},
	}

	got := renderWarnings(diag)

	assert.Equal(t, []string{
		`version skew on "opmodel.dev/catalogs/opm@v4": module requires v4.1.0, platform carries v4.0.1; rendering against the platform's build`,
		`component "web": trait "opmodel.dev/catalogs/opm/traits/expose@v4" is not handled by any matched transformer (values will be ignored)`,
	}, got)
}

// Components are worded in sorted order so the result is deterministic
// regardless of map iteration; traits keep the build's order within one.
func TestRenderWarnings_ComponentsSorted(t *testing.T) {
	diag := kernel.RenderDiagnostics{
		UnhandledTraits: map[string][]string{
			"web": {"t/b@v1", "t/a@v1"},
			"api": {"t/c@v1"},
		},
	}

	got := renderWarnings(diag)

	assert.Equal(t, []string{
		`component "api": trait "t/c@v1" is not handled by any matched transformer (values will be ignored)`,
		`component "web": trait "t/b@v1" is not handled by any matched transformer (values will be ignored)`,
		`component "web": trait "t/a@v1" is not handled by any matched transformer (values will be ignored)`,
	}, got)
}

// A render with no advisory rows words nothing.
func TestRenderWarnings_Empty(t *testing.T) {
	assert.Empty(t, renderWarnings(kernel.RenderDiagnostics{
		ResolvedVersions: []kernel.ResolvedVersion{{Path: "opmodel.dev/core@v2", ModuleVersion: "v2.0.0", PlatformVersion: "v2.0.0"}},
	}))
}
