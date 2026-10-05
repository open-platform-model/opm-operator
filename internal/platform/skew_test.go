package platform

import (
	"testing"

	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// The skew policy the Platform reconciler records beside the generated module
// (0019:D7/D18): Refuse maps to the kernel's SkewRefuse, everything else
// (Warn, unset) to the SkewWarn default. The CRD enum keeps other values out.
func TestResolveSkewPolicy(t *testing.T) {
	ptr := func(s string) *string { return &s }
	cases := []struct {
		name   string
		policy *string
		want   kernel.SkewPolicy
	}{
		{"unset resolves to Warn", nil, kernel.SkewWarn},
		{"Warn", ptr(releasesv1alpha1.SkewPolicyWarn), kernel.SkewWarn},
		{"Refuse", ptr(releasesv1alpha1.SkewPolicyRefuse), kernel.SkewRefuse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plat := &releasesv1alpha1.Platform{Spec: releasesv1alpha1.PlatformSpec{SkewPolicy: tc.policy}}
			if got := ResolveSkewPolicy(plat); got != tc.want {
				t.Fatalf("ResolveSkewPolicy = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSkewPolicyName(t *testing.T) {
	if got := SkewPolicyName(kernel.SkewWarn); got != releasesv1alpha1.SkewPolicyWarn {
		t.Fatalf("SkewPolicyName(SkewWarn) = %q, want %q", got, releasesv1alpha1.SkewPolicyWarn)
	}
	if got := SkewPolicyName(kernel.SkewRefuse); got != releasesv1alpha1.SkewPolicyRefuse {
		t.Fatalf("SkewPolicyName(SkewRefuse) = %q, want %q", got, releasesv1alpha1.SkewPolicyRefuse)
	}
}
