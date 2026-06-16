// hello release — kernel-era (opmodel.dev/core@v0) authored #ModuleRelease that
// imports the published hello test module and embeds it via #module. Exercises
// the Release CR render path (LoadReleasePackage → Compile) against a real
// imported #Module. Values live in the package (values.cue), matching the
// Release CR contract (no values on the CR).
//
// NOTE: importing a published #Module and embedding it under
// #ModuleRelease.#module re-unifies the closed module against #Module. This only
// loads cleanly on a core@v0 whose #Module declares its identity fields as
// author-supplied (modulePath!: #ModulePathType / version!: #VersionType). On
// the earlier self-referential shape it failed with
// "#module.metadata.modulePath: field not allowed". This fixture is the
// regression guard for that fix.
package hello

import (
	core "opmodel.dev/core@v0"
	hello "testing.opmodel.dev/modules/hello@v0"
)

core.#ModuleRelease

metadata: {
	name:      "hello"
	namespace: "default"
}

#module: hello
