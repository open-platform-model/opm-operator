// hello_web instance — core-v2 (opmodel.dev/core@v2) authored #ModuleInstance that
// imports the published hello_web test module and embeds it via #module. Exercises the
// ModulePackage CR render path (LoadInstancePackage → Compile) against an imported
// #Module. Values live in the package (values.cue), matching the ModulePackage CR
// contract (no values on the CR).
package hello_web

import (
	core "opmodel.dev/core@v2"
	helloweb "testing.opmodel.dev/modules/operator/hello_web@v0"
)

core.#ModuleInstance

metadata: {
	name:      "hello-web"
	namespace: "default"
}

#module: helloweb
