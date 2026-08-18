// redis instance — core-v2 (opmodel.dev/core@v2) authored #ModuleInstance that
// imports the published redis test module and embeds it via #module. Exercises the
// ModulePackage CR render path (LoadInstancePackage → Compile) against an imported
// #Module. Values live in the package (values.cue), matching the ModulePackage CR
// contract (no values on the CR).
package redis

import (
	core "opmodel.dev/core@v2"
	redis "testing.opmodel.dev/modules/operator/redis@v0"
)

core.#ModuleInstance

metadata: {
	name:      "redis"
	namespace: "default"
}

#module: redis
