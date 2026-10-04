// Package opm_operator is the opm-operator's own OPM module: its four CRDs,
// its Namespace, the controller Deployment with its ServiceAccount and
// metrics Service, and every Role, ClusterRole and binding the operator
// ships, each through the first-party catalog resource made for its kind.
//
//	module.cue             metadata, #config, debugValues, the instance guard
//	components.cue         the components
//	operator/operator.cue  the operator release this module version deploys
//	zz_generated_crds.cue  #crdSource, cue-imported from config/crd/bases (generated)
//	zz_generated_rbac.cue  #rbacSource, cue-imported from config/rbac (generated)
//
// The operator is a cluster singleton, and the cli locates an operator by
// its fixed names, so every name is a constant and the module renders only
// for the instance opm-operator in opm-operator-system.
package opm_operator

import (
	"strconv"
	"strings"

	m "opmodel.dev/core@v2"
	res "opmodel.dev/catalogs/opm/resources/v1beta1"

	id "opmodel.dev/modules/opm_operator/identity"
	op "opmodel.dev/modules/opm_operator/operator"
)

m.#Module

// Re-declared so components.cue can read it: a field of the embedded
// #Module is not in lexical scope from another file.
#ctx: _

// The only instance coordinates the module renders for. Any other name or
// namespace fails here, naming the given and the expected coordinates.
_instanceGuard: "\(#ctx.instance.namespace)/\(#ctx.instance.name)" & "\(_namespace)/\(_instance)"

_instance:  "opm-operator"
_namespace: "opm-operator-system"

metadata: {
	_segments:   strings.Split(strings.SplitN(id.ModulePath, "@", 2)[0], "/")
	name:        _segments[len(_segments)-1]
	modulePath:  id.ModulePath
	version:     id.Version
	description: "The opm-operator: its CRDs, RBAC, metrics Service and controller Deployment"
}

// The operator's tuning surface. Closed: the image tag, digest and pull
// policy are not values, because a recorded tag would outlive a module
// upgrade; the module names its operator release in operator/operator.cue.
#config: {
	// The image repository, for a mirror. The tag and digest stay the
	// module's: "<repository>:v<operator.Version>@<digest>".
	image: repository: string & !="" | *op.Image.repository

	// --registry: the CUE registry mapping the operator resolves modules
	// through. Unset leaves the flag out.
	registry?: string & !=""

	// --default-service-account: the identity the operator applies as when
	// a ModuleInstance names none. Unset leaves the flag out.
	defaultServiceAccount?: string & !=""

	// The manager container's resources. Each quantity defaults on its own,
	// so setting one keeps the others. The memory limit is always set, in Mi
	// or Gi, and GOMEMLIMIT follows it (80 percent, in MiB).
	resources: res.#ResourceRequirementsSchema & {
		requests: cpu:    _ | *"100m"
		requests: memory: _ | *"256Mi"
		limits: cpu:      _ | *2
		limits: memory:   _ | *"4Gi"
	}
	resources: limits: memory: string | error("resources.limits.memory: give the memory limit in Mi or Gi, such as \"4Gi\"; a plain byte count is refused")

	replicas: int & >=1 | *1

	// Further controller arguments, after every argument the module renders.
	// The registry mapping and the default service account have typed values
	// and the module's own arguments are fixed, in either flag spelling.
	extraArgs: [...#extraArg] | *[]
}

#extraArg: A={
	string
	[
		if A =~ "^--?registry(=|$)" {error("extraArgs: \"\(A)\" sets the registry mapping; set #config.registry instead")},
		if A =~ "^--?default-service-account(=|$)" {error("extraArgs: \"\(A)\" sets the default service account; set #config.defaultServiceAccount instead")},
		if A =~ "^--?(metrics-bind-address|leader-elect|health-probe-bind-address)(=|$)" {error("extraArgs: \"\(A)\" overrides an argument the module renders itself")},
		_,
	][0]
}

// The defaults render the operator as the earlier install manifest did.
debugValues: {}

// GOMEMLIMIT: the floor of 80 percent of the resolved memory limit, in MiB.
_memoryLimit: #config.resources.limits.memory
_memoryLimitMiB: [
	if strings.HasSuffix(_memoryLimit, "Gi") {strconv.Atoi(strings.TrimSuffix(_memoryLimit, "Gi")) * 1024},
	if strings.HasSuffix(_memoryLimit, "Mi") {strconv.Atoi(strings.TrimSuffix(_memoryLimit, "Mi"))},
][0]
_goMemLimit: "\(div(_memoryLimitMiB*4, 5))MiB"
