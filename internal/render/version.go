package render

import (
	"cuelang.org/go/cue"

	"github.com/open-platform-model/library/opm/module"
	"github.com/open-platform-model/library/opm/schema"
)

// moduleVersionPath is version under a module's metadata. It is a concrete
// field, so it is parsed rather than built with cue.Def.
var moduleVersionPath = cue.ParsePath("version")

// declaredModuleVersion returns the version the instance's source module
// declares in #module.metadata.version, as the module spells it (bare
// SemVer), or "" when it cannot be read as a concrete string.
//
// It reads the rendered instance rather than the version the caller asked
// for, so a ModuleInstance and a ModulePackage report the same thing in the
// same spelling. The version is informational: core and the kernel's loader
// already refuse a module whose metadata is not concrete, and an unreadable
// value must never fail a render, so every failure collapses to "".
func declaredModuleVersion(inst *module.Instance) string {
	if inst == nil {
		return ""
	}
	v := inst.Package.LookupPath(schema.Module).LookupPath(schema.Metadata).LookupPath(moduleVersionPath)
	s, err := v.String()
	if err != nil {
		return ""
	}
	return s
}
