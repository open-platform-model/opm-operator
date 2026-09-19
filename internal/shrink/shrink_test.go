/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package shrink

import (
	"context"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

func shrinkScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = releasesv1alpha1.AddToScheme(s)
	return s
}

// claimName is the dot-joined "<namespace>.<name>" every claim in these
// tests carries; the join is the CRD's own naming rule.
const claimName = "team-a.provider"

// renderedClaim builds the unstructured a provider render produces.
func renderedClaim(provides ...string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": releasesv1alpha1.GroupVersion.String(),
		"kind":       claimKind,
		"metadata":   map[string]any{"name": claimName},
		"spec": map[string]any{
			"catalog": "opmodel.dev/catalogs/example@v1",
			"version": "2.0.0",
		},
	}}
	values := make([]any, 0, len(provides))
	for _, fqn := range provides {
		values = append(values, fqn)
	}
	_ = unstructured.SetNestedSlice(u.Object, values, "spec", "provides")
	return u
}

func storedClaim(provides ...string) *releasesv1alpha1.TransformerRegistration {
	return &releasesv1alpha1.TransformerRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: claimName},
		Spec: releasesv1alpha1.TransformerRegistrationSpec{
			Catalog:  "opmodel.dev/catalogs/example@v1",
			Version:  "1.0.0",
			Provides: provides,
		},
	}
}

func demandingInstance(namespace, name string, contracts ...string) *releasesv1alpha1.ModuleInstance {
	return &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status:     releasesv1alpha1.ModuleInstanceStatus{RequiredContracts: contracts},
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name           string
		rendered       *unstructured.Unstructured
		objects        []runtime.Object
		wantRefused    bool
		wantDropped    []string
		wantDependents []string
	}{
		{
			// The case the guard exists for.
			name:     "a shrink dependents still demand is refused",
			rendered: renderedClaim("opmodel.dev/contract.Storage"),
			objects: []runtime.Object{
				storedClaim("opmodel.dev/contract.Storage", "opmodel.dev/contract.Backup"),
				demandingInstance("team-b", "needs-backup", "opmodel.dev/contract.Backup"),
				demandingInstance("team-c", "needs-storage", "opmodel.dev/contract.Storage"),
			},
			wantRefused:    true,
			wantDropped:    []string{"opmodel.dev/contract.Backup"},
			wantDependents: []string{"team-b/needs-backup"},
		},
		{
			// Dropping a contract nobody demands is an ordinary upgrade.
			name:     "a shrink nobody demands is allowed",
			rendered: renderedClaim("opmodel.dev/contract.Storage"),
			objects: []runtime.Object{
				storedClaim("opmodel.dev/contract.Storage", "opmodel.dev/contract.Backup"),
				demandingInstance("team-c", "needs-storage", "opmodel.dev/contract.Storage"),
			},
		},
		{
			// The common case 0015:D16 names explicitly: same set, new build.
			name:     "the same contract set at a new version is allowed",
			rendered: renderedClaim("opmodel.dev/contract.Backup", "opmodel.dev/contract.Storage"),
			objects: []runtime.Object{
				storedClaim("opmodel.dev/contract.Storage", "opmodel.dev/contract.Backup"),
				demandingInstance("team-b", "needs-backup", "opmodel.dev/contract.Backup"),
			},
		},
		{
			// Nothing stored means nothing can be taken away.
			name:     "a claim the cluster does not hold is allowed",
			rendered: renderedClaim("opmodel.dev/contract.Storage"),
			objects: []runtime.Object{
				demandingInstance("team-b", "needs-backup", "opmodel.dev/contract.Backup"),
			},
		},
		{
			// Instances exist, but none of them depends on the provider.
			name:     "a shrink with no instance demanding anything is allowed",
			rendered: renderedClaim("opmodel.dev/contract.Storage"),
			objects: []runtime.Object{
				storedClaim("opmodel.dev/contract.Storage", "opmodel.dev/contract.Backup"),
				demandingInstance("team-b", "demands-nothing"),
			},
		},
		{
			// Emptying provides removes every stored contract at once.
			name:     "emptying provides is refused for every demanded contract",
			rendered: renderedClaim(),
			objects: []runtime.Object{
				storedClaim("opmodel.dev/contract.Storage", "opmodel.dev/contract.Backup"),
				demandingInstance("team-b", "needs-both",
					"opmodel.dev/contract.Backup", "opmodel.dev/contract.Storage"),
			},
			wantRefused: true,
			wantDropped: []string{
				"opmodel.dev/contract.Backup",
				"opmodel.dev/contract.Storage",
			},
			wantDependents: []string{"team-b/needs-both"},
		},
		{
			// Every other rendered resource is judged by nothing at all.
			name: "a resource that is not a claim is allowed",
			rendered: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{"name": claimName, "namespace": "team-a"},
			}},
			objects: []runtime.Object{
				storedClaim("opmodel.dev/contract.Storage", "opmodel.dev/contract.Backup"),
				demandingInstance("team-b", "needs-backup", "opmodel.dev/contract.Backup"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(shrinkScheme()).
				WithRuntimeObjects(tt.objects...).
				Build()
			decider := &Decider{Client: c, APIReader: c}

			decision, err := decider.Decide(context.Background(), tt.rendered)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if decision.Refused() != tt.wantRefused {
				t.Fatalf("Refused() = %t, want %t (decision %+v)", decision.Refused(), tt.wantRefused, decision)
			}
			if !tt.wantRefused {
				if decision.Claim != "" || len(decision.Dropped) > 0 || len(decision.Dependents) > 0 {
					t.Fatalf("allowed decision is not zero: %+v", decision)
				}
				return
			}
			if decision.Claim != claimName {
				t.Errorf("Claim = %q, want %q", decision.Claim, claimName)
			}
			if !slices.Equal(decision.Dropped, tt.wantDropped) {
				t.Errorf("Dropped = %v, want %v", decision.Dropped, tt.wantDropped)
			}
			if !slices.Equal(decision.Dependents, tt.wantDependents) {
				t.Errorf("Dependents = %v, want %v", decision.Dependents, tt.wantDependents)
			}
		})
	}
}

// A dropped contract several instances demand names all of them, so the
// reported count is the count an operator has to act on.
func TestEvaluate_NamesEveryDependent(t *testing.T) {
	decision := evaluate(
		"team-a.provider",
		[]string{"opmodel.dev/contract.Backup"},
		[]releasesv1alpha1.ModuleInstance{
			*demandingInstance("team-c", "second", "opmodel.dev/contract.Backup"),
			*demandingInstance("team-b", "first", "opmodel.dev/contract.Backup"),
			*demandingInstance("team-d", "unrelated", "opmodel.dev/contract.Storage"),
		},
	)

	if !decision.Refused() {
		t.Fatalf("expected a refusal, got %+v", decision)
	}
	want := []string{"team-b/first", "team-c/second"}
	if !slices.Equal(decision.Dependents, want) {
		t.Errorf("Dependents = %v, want %v", decision.Dependents, want)
	}
}

// An instance demanding a dropped contract twice is one dependent, not two.
func TestEvaluate_CountsAnInstanceOnce(t *testing.T) {
	decision := evaluate(
		"team-a.provider",
		[]string{"opmodel.dev/contract.Backup", "opmodel.dev/contract.Storage"},
		[]releasesv1alpha1.ModuleInstance{
			*demandingInstance("team-b", "needs-both",
				"opmodel.dev/contract.Backup", "opmodel.dev/contract.Storage"),
		},
	)

	if len(decision.Dependents) != 1 {
		t.Fatalf("Dependents = %v, want one entry", decision.Dependents)
	}
	if len(decision.Dropped) != 2 {
		t.Fatalf("Dropped = %v, want both contracts", decision.Dropped)
	}
}

// Only the demanded half of a multi-contract shrink is reported: the rest is
// an ordinary removal and naming it would send an operator after nothing.
func TestEvaluate_ReportsOnlyDemandedContracts(t *testing.T) {
	decision := evaluate(
		"team-a.provider",
		[]string{"opmodel.dev/contract.Backup", "opmodel.dev/contract.Unused"},
		[]releasesv1alpha1.ModuleInstance{
			*demandingInstance("team-b", "needs-backup", "opmodel.dev/contract.Backup"),
		},
	)

	want := []string{"opmodel.dev/contract.Backup"}
	if !slices.Equal(decision.Dropped, want) {
		t.Errorf("Dropped = %v, want %v", decision.Dropped, want)
	}
}
