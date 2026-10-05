package platform

import (
	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// SingletonName is the only permitted name for the cluster-scoped Platform
// singleton. The CRD enforces it with a CEL rule; the Platform reconciler
// guards on it again, and the render input key reads the Platform by it.
const SingletonName = "cluster"

// ResolveSkewPolicy resolves spec.skewPolicy to the kernel's policy: Refuse
// maps to SkewRefuse, anything else (Warn, unset) to SkewWarn, the 0019:D18
// default. The CRD enum keeps other values out at admission.
func ResolveSkewPolicy(plat *releasesv1alpha1.Platform) kernel.SkewPolicy {
	if plat.Spec.SkewPolicy != nil && *plat.Spec.SkewPolicy == releasesv1alpha1.SkewPolicyRefuse {
		return kernel.SkewRefuse
	}
	return kernel.SkewWarn
}

// SkewPolicyName spells a kernel skew policy the way Platform.spec.skewPolicy
// spells it: "Refuse" for SkewRefuse, "Warn" for anything else. The render
// input key carries this spelling, so the key does not depend on the
// library's enum values.
func SkewPolicyName(p kernel.SkewPolicy) string {
	if p == kernel.SkewRefuse {
		return releasesv1alpha1.SkewPolicyRefuse
	}
	return releasesv1alpha1.SkewPolicyWarn
}

// PinSet is the Platform field that names the generated package a render
// consumes: status.packageIdentity. The render input key hashes it and the
// ModuleInstance watch predicate wakes on it; both read it here, so the two
// cannot drift apart.
func PinSet(plat *releasesv1alpha1.Platform) string {
	return plat.Status.PackageIdentity
}
