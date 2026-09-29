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
	"slices"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// subscribedContract returns the first contract the claim provides that some
// OTHER enabled registry entry already provides, with that entry's registry
// key, or two empty strings when none does.
//
// providers is the built platform's provider count
// (ContractInventory.ProvidedBy): contract FQN to the sorted registry keys of
// the enabled entries providing it. A registry key is the catalog module path
// with its major, the same string a claim's spec.catalog carries, so
// ownCatalog matches the claim's own entry and nothing else: another major of
// the same catalog is another registry entry, and so another provider.
//
// Every provider matching ownCatalog is skipped. Once a claim is active its
// catalog is a registry entry of the very platform the next reconcile judges
// it against (0015:D13), so without this a re-judged claim would be refused
// for providing what it itself provides, be deactivated, drop out of the next
// package and be accepted again, the oscillation 0015:D13's loop has to
// avoid. A claim's own contracts are what it is for; only another provider of
// them is a conflict.
//
// The claim's contracts are walked in sorted order and each contract's
// providers in the count's own sorted order, so a claim colliding on several
// always reports the same one rather than whichever the slice happened to
// list first.
func subscribedContract(provides []string, providers map[string][]string, ownCatalog string) (contract, entry string) {
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
// Only active claims hold. An accepted but inactive claim has never served, so
// it has no dependents to protect, and letting it block a competitor would let
// a provider that never came up lock out one that did (0015:D2).
//
// A claim on its way out does not hold either — unless its deletion is
// BLOCKED, in which case it is not on its way out: it is still serving the
// dependents the block protects, and its catalog is still a registry entry of
// the generated platform. Accepting a second provider of the same contract
// while that is true does not produce a migration, it produces an
// over-subscribed platform whose generation the single-provider guard refuses
// cluster-wide (0010:D32/D37). Refusing the newcomer here names the claim that
// holds the contract and the dependents holding it open, which is the same
// rule the 0015:D12 check one file over now reads.
//
// Claims are walked in name order and each one's contracts in sorted order, so
// the reported conflict does not move between reconciles.
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
