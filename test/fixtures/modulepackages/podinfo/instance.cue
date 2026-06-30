// podinfo instance — kernel-era (opmodel.dev/core@v1) authored #ModuleInstance that
// imports the published podinfo test module and embeds it via #module. Exercises the
// ModulePackage CR render path (LoadInstancePackage → Compile) against an imported
// #Module. Values live in the package (values.cue), matching the ModulePackage CR
// contract (no values on the CR).
package podinfo

import (
	core "opmodel.dev/core@v1"
	podinfo "opmodel.dev/modules/test/podinfo@v0"
)

core.#ModuleInstance

metadata: {
	name:      "podinfo"
	namespace: "default"
}

#module: podinfo
