// hello instance — kernel-era (opmodel.dev/core@v1) authored #ModuleInstance that
// imports the published hello test module and embeds it via #module. Exercises
// the ModulePackage CR render path (LoadInstancePackage → Compile) against a real
// imported #Module. Values live in the package (values.cue), matching the
// ModulePackage CR contract (no values on the CR).
//
// NOTE: importing a published #Module and embedding it under
// #ModuleInstance.#module re-unifies the closed module against #Module. This only
// loads cleanly on a core whose #Module declares its identity fields as
// author-supplied (modulePath!: #ModulePathType / version!: #VersionType). On
// the earlier self-referential shape it failed with
// "#module.metadata.modulePath: field not allowed". This fixture is the
// regression guard for that fix.
package hello

import (
	core "opmodel.dev/core@v1"
	hello "opmodel.dev/modules/test/hello@v0"
)

core.#ModuleInstance

metadata: {
	name:      "hello"
	namespace: "default"
}

#module: hello
