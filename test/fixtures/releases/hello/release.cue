package hello

import (
	mr "opmodel.dev/core/v1alpha1/modulerelease@v1"
	hello "testing.opmodel.dev/modules/hello@v0"
)

mr.#ModuleRelease

metadata: {
	name:      "hello"
	namespace: "default"
}

#module: hello
