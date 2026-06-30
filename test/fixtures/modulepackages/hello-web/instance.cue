// hello-web instance — kernel-era (opmodel.dev/core@v1) authored #ModuleInstance that
// imports the published hello-web test module and embeds it via #module. Exercises the
// ModulePackage CR render path (LoadInstancePackage → Compile) against an imported
// #Module. Values live in the package (values.cue), matching the ModulePackage CR
// contract (no values on the CR).
package hello_web

import (
	core "opmodel.dev/core@v1"
	// The module's CUE package is hello_web (underscore); the import path's last
	// element hello-web is not a valid CUE identifier, so name the package explicitly.
	helloweb "opmodel.dev/modules/test/hello-web@v0:hello_web"
)

core.#ModuleInstance

metadata: {
	name:      "hello-web"
	namespace: "default"
}

#module: helloweb
