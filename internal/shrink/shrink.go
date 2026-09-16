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

// Package shrink answers one question about a rendered
// TransformerRegistration: does applying it take a contract away from
// instances that still demand it (enhancement 0015 D16)?
//
// The answer is a decision, not an action. The caller decides what to do with
// a refusal; this package neither reads nor writes anything beyond the two
// facts it compares.
package shrink

import (
	"context"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// claimKind is the kind of the rendered resource this package judges.
const claimKind = "TransformerRegistration"

// Decision is the verdict on one rendered TransformerRegistration.
// The zero value refuses nothing, which is the verdict for every resource
// that is not a claim.
type Decision struct {
	// Claim is the metadata.name of the rendered claim, empty when nothing
	// was judged.
	Claim string

	// Dropped lists the contract FQNs the stored claim provides, the rendered
	// one does not, and instances still demand — in FQN order.
	Dropped []string

	// Dependents names the instances demanding at least one dropped contract,
	// as "<namespace>/<name>" in name order.
	Dependents []string
}

// Refused reports whether the rendered claim must be withheld from apply.
//
// The two lists move together: a contract reaches Dropped only when an
// instance demands it, so a non-empty Dropped implies a non-empty Dependents.
func (d Decision) Refused() bool {
	return len(d.Dropped) > 0
}

// isClaim reports whether a rendered resource is a TransformerRegistration,
// the only kind this package judges.
func isClaim(resource *unstructured.Unstructured) bool {
	gvk := resource.GroupVersionKind()
	return gvk.Group == releasesv1alpha1.GroupVersion.Group && gvk.Kind == claimKind
}

// Decider judges a rendered claim against what the cluster holds.
type Decider struct {
	// Client lists the instances whose demand is compared. The cached client
	// is correct here: demand that is one reconcile stale over-reports, which
	// refuses an upgrade that could have proceeded — the direction a guard
	// should fail in.
	Client client.Reader

	// APIReader reads the stored claim, uncached. The comparison against the
	// stored provides is the whole verdict, so a cached read that missed the
	// last write would compare against a set the cluster no longer holds and
	// reach a verdict about nothing.
	APIReader client.Reader
}

// Decide judges one rendered resource.
//
// A resource that is not a claim, a claim the cluster does not hold yet, and
// a claim whose provides did not shrink all return the zero Decision without
// listing anything: the common upgrade pays for one uncached read and no
// more.
func (d *Decider) Decide(
	ctx context.Context,
	rendered *unstructured.Unstructured,
) (Decision, error) {
	if !isClaim(rendered) {
		return Decision{}, nil
	}

	renderedProvides, err := provides(rendered)
	if err != nil {
		return Decision{}, fmt.Errorf("reading provides of rendered claim %s: %w", rendered.GetName(), err)
	}

	var stored releasesv1alpha1.TransformerRegistration
	if err := d.APIReader.Get(ctx, types.NamespacedName{Name: rendered.GetName()}, &stored); err != nil {
		if apierrors.IsNotFound(err) {
			// A claim the cluster does not hold takes nothing away.
			return Decision{}, nil
		}
		return Decision{}, fmt.Errorf("reading stored claim %s: %w", rendered.GetName(), err)
	}

	removed := removedContracts(stored.Spec.Provides, renderedProvides)
	if len(removed) == 0 {
		return Decision{}, nil
	}

	var list releasesv1alpha1.ModuleInstanceList
	if err := d.Client.List(ctx, &list); err != nil {
		return Decision{}, fmt.Errorf("listing module instances: %w", err)
	}

	return evaluate(rendered.GetName(), removed, list.Items), nil
}

// evaluate is the decision itself, over facts already read: the claim's name,
// the contracts its rendered provides removed, and the instances whose demand
// decides whether removing them abandons anyone.
//
// Demand is read from each instance's status.requiredContracts, the same
// count the deletion guard uses (internal/controller, ClaimFinalizerName), so
// both doors refuse on one definition of "who depends on this contract".
// Nothing is rendered and nothing is acquired from a registry.
//
// The provider's own instance is not special-cased. Its stored demand is one
// reconcile behind its render during the reconcile that produced this claim,
// so a provider that has just stopped demanding a contract it also provides
// blocks itself once. That reconcile commits the fresh demand along with the
// refusal, so the next one releases: over-blocking that heals itself, rather
// than a second definition of demand.
func evaluate(
	claim string,
	removed []string,
	instances []releasesv1alpha1.ModuleInstance,
) Decision {
	demanded := make(map[string]struct{}, len(removed))
	var dependents []string

	for i := range instances {
		instance := &instances[i]
		claims := false
		for _, fqn := range instance.Status.RequiredContracts {
			if slices.Contains(removed, fqn) {
				demanded[fqn] = struct{}{}
				claims = true
			}
		}
		if claims {
			dependents = append(dependents, instance.Namespace+"/"+instance.Name)
		}
	}

	if len(demanded) == 0 {
		// Every removed contract is unwanted. Dropping it abandons nobody.
		return Decision{}
	}

	dropped := make([]string, 0, len(demanded))
	for fqn := range demanded {
		dropped = append(dropped, fqn)
	}
	slices.Sort(dropped)
	slices.Sort(dependents)

	return Decision{Claim: claim, Dropped: dropped, Dependents: dependents}
}

// removedContracts returns the FQNs present in stored and absent from
// rendered, in stored order. A rendered claim that adds contracts, or names
// the same set at a new catalog version, removes nothing.
func removedContracts(stored, rendered []string) []string {
	var removed []string
	for _, fqn := range stored {
		if !slices.Contains(rendered, fqn) {
			removed = append(removed, fqn)
		}
	}
	return removed
}

// provides reads spec.provides off a rendered claim. An absent field reads as
// the empty set, which the CRD allows and which removes every stored
// contract — the safe direction for a guard.
func provides(rendered *unstructured.Unstructured) ([]string, error) {
	values, _, err := unstructured.NestedStringSlice(rendered.Object, "spec", "provides")
	if err != nil {
		return nil, err
	}
	return values, nil
}
