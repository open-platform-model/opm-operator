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

package controller

import (
	"context"
	"fmt"
	"slices"

	"cuelang.org/go/cue"

	"github.com/open-platform-model/library/opm/platform"
	"github.com/open-platform-model/library/opm/schema"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// composedTransformers is #Platform.#composedTransformers, core's fold of
// every ENABLED registry entry's transformers. Reading the fold rather than
// the registry is what makes "enabled" structural here: a disabled
// subscription contributes nothing to it, so it cannot hold a contract
// against a claim.
var composedTransformers = cue.MakePath(cue.Def("composedTransformers"))

// transformerModulePath is the catalog stamp core identifies a transformer's
// owning catalog by (enhancement 0010 D25). The stamp is unforgeable, so a
// catalog cannot pose as two providers.
var transformerModulePath = cue.ParsePath("metadata.modulePath")

// subscriptionProviders folds the provider-fulfilled contracts the platform's
// enabled subscriptions implement: contract FQN to the module path of the
// catalog implementing it.
//
// It is derived the same way core derives #contracts._providers — a catalog
// provides a contract when one of its transformers REQUIRES that contract
// with fulfilment "provider" — and the same way the library derives a
// catalog's own provider set, by reading the demand entry's fulfilment rather
// than looking the contract up in the catalog's member maps. A provider
// catalog implements contracts another catalog defines, so its own maps do
// not list them (library ADR-009).
//
// The inventory the library decodes (platform.Platform.Contracts) does not
// carry this map: core keeps the per-contract provider set in a hidden field
// and exports only the two reports folded from it (unfulfilled,
// overSubscribed), neither of which says WHICH catalog provides a contract.
// That is the value D2's refusal has to name.
//
// Every provider of a contract is recorded, not just one, because the claim
// being judged may be one of them: an ACTIVE claim's catalog is a registry
// entry of the platform it is judged against (enhancement 0015 D13), so its
// own transformers show up here. The caller drops the claim's own catalog and
// judges against what remains; keeping only one provider per contract would
// make that a coin toss. The paths are sorted, so the caller reports the
// lowest competing one rather than whichever the map iteration surfaced.
func subscriptionProviders(p *platform.Platform) (map[string][]string, error) {
	if p == nil {
		return nil, fmt.Errorf("no platform has been built")
	}

	composed := p.Package.LookupPath(composedTransformers)
	if !composed.Exists() {
		return nil, fmt.Errorf("platform carries no %s field", composedTransformers)
	}
	if err := composed.Err(); err != nil {
		return nil, fmt.Errorf("platform %s did not evaluate: %w", composedTransformers, err)
	}

	iter, err := composed.Fields()
	if err != nil {
		return nil, fmt.Errorf("reading platform %s: %w", composedTransformers, err)
	}

	providers := map[string][]string{}
	for iter.Next() {
		impl := iter.Selector().Unquoted()

		pathValue := iter.Value().LookupPath(transformerModulePath)
		if !pathValue.Exists() {
			return nil, fmt.Errorf("transformer %q carries no %s", impl, transformerModulePath)
		}
		modulePath, err := pathValue.String()
		if err != nil {
			return nil, fmt.Errorf("reading %s of transformer %q: %w", transformerModulePath, impl, err)
		}

		for _, demands := range []cue.Path{schema.RequiredResources, schema.RequiredTraits} {
			if err := collectProvided(iter.Value(), impl, demands, modulePath, providers); err != nil {
				return nil, err
			}
		}
	}
	for fqn, paths := range providers {
		slices.Sort(paths)
		providers[fqn] = slices.Compact(paths)
	}
	return providers, nil
}

// collectProvided records modulePath among the providers of every contract
// the transformer's demand map at path requires with fulfilment "provider".
// An absent demand map contributes nothing: both maps are optional on
// #ComponentTransformer.
func collectProvided(
	transformer cue.Value,
	impl string,
	path cue.Path,
	modulePath string,
	providers map[string][]string,
) error {
	demands := transformer.LookupPath(path)
	if !demands.Exists() {
		return nil
	}
	iter, err := demands.Fields()
	if err != nil {
		return fmt.Errorf("reading %s of transformer %q: %w", path, impl, err)
	}
	for iter.Next() {
		fqn := iter.Selector().Unquoted()
		fulfilment := iter.Value().LookupPath(schema.Fulfilment)
		if !fulfilment.Exists() {
			continue
		}
		s, err := fulfilment.String()
		if err != nil {
			return fmt.Errorf("reading %s of contract %q required by transformer %q: %w",
				schema.Fulfilment, fqn, impl, err)
		}
		if s != "provider" {
			continue
		}
		providers[fqn] = append(providers[fqn], modulePath)
	}
	return nil
}

// subscribedContract returns the first contract the claim provides that some
// OTHER enabled registry entry already provides, with the catalog holding it,
// or two empty strings when none does.
//
// ownCatalog is the claim's own catalog path, and every provider matching it
// is skipped. Once a claim is active its catalog is a registry entry of the
// very platform the next reconcile judges it against (enhancement 0015 D13),
// so without this a re-judged claim would be refused for providing what it
// itself provides, be deactivated, drop out of the next package and be
// accepted again — the oscillation D13's loop has to avoid. A claim's own
// contracts are what it is for; only another provider of them is a conflict.
//
// The claim's contracts are walked in sorted order and each contract's
// providers likewise, so a claim colliding on several always reports the same
// one rather than whichever the slice happened to list first.
func subscribedContract(provides []string, providers map[string][]string, ownCatalog string) (contract, catalogPath string) {
	for _, fqn := range slices.Sorted(slices.Values(provides)) {
		for _, held := range providers[fqn] {
			if held == ownCatalog {
				continue
			}
			return fqn, held
		}
	}
	return "", ""
}

// activeContractHolder returns the first contract the claim provides that
// another ACTIVE claim already provides, with the holder's name, or two empty
// strings when none does.
//
// Only active claims hold. An accepted but inactive claim has never served,
// so it has no dependents to protect, and letting it block a competitor would
// let a provider that never came up lock out one that did (enhancement 0015
// D2).
//
// A claim on its way out does not hold either — unless its deletion is
// BLOCKED, in which case it is not on its way out: it is still serving the
// dependents the block protects, and its catalog is still a registry entry of
// the generated platform. Accepting a second provider of the same contract
// while that is true does not produce a migration, it produces an
// over-subscribed platform whose generation the single-provider guard refuses
// cluster-wide (0010 D32/D37). Refusing the newcomer here names the claim
// that holds the contract and the dependents holding it open, which is the
// same rule the D12 check one file over now reads.
//
// Claims are walked in name order and each one's contracts in sorted order,
// so the reported conflict does not move between reconciles.
func (r *TransformerRegistrationReconciler) activeContractHolder(
	ctx context.Context,
	claim *releasesv1alpha1.TransformerRegistration,
) (contract, holder string, err error) {
	var list releasesv1alpha1.TransformerRegistrationList
	if err := r.List(ctx, &list); err != nil {
		return "", "", err
	}

	claimed := slices.Sorted(slices.Values(claim.Spec.Provides))

	others := make([]*releasesv1alpha1.TransformerRegistration, 0, len(list.Items))
	for i := range list.Items {
		other := &list.Items[i]
		if other.Name == claim.Name || !other.Status.Active {
			continue
		}
		if !other.DeletionTimestamp.IsZero() && !claimContributesAfterDeletion(other) {
			continue
		}
		others = append(others, other)
	}
	slices.SortFunc(others, func(a, b *releasesv1alpha1.TransformerRegistration) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	for _, other := range others {
		held := make(map[string]struct{}, len(other.Spec.Provides))
		for _, fqn := range other.Spec.Provides {
			held[fqn] = struct{}{}
		}
		for _, fqn := range claimed {
			if _, ok := held[fqn]; ok {
				return fqn, other.Name, nil
			}
		}
	}
	return "", "", nil
}
