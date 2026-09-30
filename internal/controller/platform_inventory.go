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
	"fmt"
	"slices"
	"strings"

	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/open-platform-model/library/opm/platform"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// The three verdicts the Platform reconciler reads off a built platform's
// contract inventory: colliding contract keys, over-subscribed contracts and
// comparable transformer pairs (0015:D5, D18). All are pure functions of the
// inventory: they decide and they word, they never read a registry or touch
// the store, which is what lets every message be pinned by a table test
// without a live catalog pair producing it.
//
// Nothing here counts providers or definers. The inventory's provider count
// (ProvidedBy, with OverSubscribed and Routable derived from it) is the one
// core computes per registry entry (catalog path plus major) and the render
// build refuses on, and its collision report (Collisions, CollidingEntries)
// is core's count of enabled entries defining each contract key; the
// operator reads both, so the generation gate and the render refusal fire on
// the same platforms.
//
// Every list a finding prints is sorted first, into a copy. Core
// derives the inventory in comprehension order, which is stable for one build
// but says nothing across builds, and failReconcile gates its warning event
// on the message being unchanged, so an unsorted list would re-fire the event
// on a reconcile that found the very same problem. The inventory itself is
// never mutated: the store may hold it.

// inventoryRefusal decides whether a built platform may be recorded. It
// returns the Ready reason, the message and true when the package must be
// withheld, and zero values when the inventory is routable, discriminated
// and free of collisions.
//
// The gate is the inventory's own verdicts plus its collision list. Routable
// and Discriminated are what core computed; a non-empty Collisions refuses
// whatever Routable reads, because a colliding key is absent from DefinedBy,
// RequiredBy and Comparable, so nothing else in the inventory can be trusted
// to have caught the problem (Fulfilled and Discriminated can read true
// under a collision). Each finding prints only when its list is non-empty.
// A Routable false that no list explains still refuses, under the routing
// reason, with a finding that claims no count (fail closed).
//
// Precedence is ContractCollisions, then OverSubscribedContracts, then
// ComparablePredicates. One condition carries one reason: a collision comes
// first because it distorts the other reports and its fix (disabling a
// major) reshapes them, and 0015:D18 fixes over-subscription as the
// generation refusal with 0015:D5 joining it. Every finding stays in the
// message, the collision first, so an operator fixing one problem sees the
// others in the same message rather than one reconcile later.
func inventoryRefusal(inv *platform.ContractInventory) (reason, msg string, refused bool) {
	var findings []string
	if len(inv.Collisions) > 0 {
		findings = append(findings, collisionFinding(inv))
		reason = status.ContractCollisionsReason
	}
	if len(inv.OverSubscribed) > 0 {
		findings = append(findings, overSubscribedFinding(inv))
		if reason == "" {
			reason = status.OverSubscribedContractsReason
		}
	}
	if !inv.Routable && len(inv.Collisions) == 0 && len(inv.OverSubscribed) == 0 {
		findings = append(findings, unroutableFinding)
		reason = status.OverSubscribedContractsReason
	}
	if !inv.Discriminated {
		findings = append(findings, comparableFinding(inv))
		if reason == "" {
			reason = status.ComparablePredicatesReason
		}
	}
	if len(findings) == 0 {
		return "", "", false
	}
	return reason, strings.Join(findings, "\n\n"), true
}

// collisionFinding words the collision refusal: each contract key more than
// one enabled registry entry defines, and every entry defining it (two majors
// of one catalog sharing keys, say). The entries are read from
// CollidingEntries, never derived from DefinedBy (which lacks a colliding key
// by construction) and never counted here. A registry entry is what the
// remedy acts on: a spec.registry key to disable. A key CollidingEntries
// lacks (an inconsistent inventory) still prints, with an empty list, and
// still refuses.
func collisionFinding(inv *platform.ContractInventory) string {
	keys := slices.Sorted(slices.Values(inv.Collisions))

	var b strings.Builder
	fmt.Fprintf(&b, "platform is not routable: %s; a platform package cannot be generated until all but one of the registry entries defining each is disabled:",
		counted(len(keys), "colliding contract", "colliding contracts"))
	for _, key := range keys {
		entries := slices.Sorted(slices.Values(inv.CollidingEntries[key]))
		fmt.Fprintf(&b, "\n  %s defined by %s", key, strings.Join(entries, ", "))
	}
	return b.String()
}

// unroutableFinding is the fail-closed backstop's finding: core read the
// platform not routable and the inventory names no over-subscribed or
// colliding contract to explain it (a term in routable this operator does
// not know). It never claims a count, so it never reads "0 over-subscribed".
const unroutableFinding = "platform is not routable; the contract inventory names no over-subscribed or colliding contract, " +
	"so a platform package cannot be generated"

// overSubscribedFinding words the routing refusal: each over-subscribed
// contract, the catalog defining it when an enabled one does, and every
// enabled registry entry providing it (0010:D37, kept by 0015:D2). The
// providers are read from ProvidedBy, never counted here: a contract is
// over-subscribed when two or more registry entries (two majors of one
// catalog are two) provide it, whether or not its defining catalog is
// enabled. A registry entry is what the remedy acts on, a spec.registry key
// to disable or a claim's spec.catalog to withdraw.
func overSubscribedFinding(inv *platform.ContractInventory) string {
	contracts := slices.Sorted(slices.Values(inv.OverSubscribed))

	var b strings.Builder
	fmt.Fprintf(&b, "platform is not routable: %s; a platform package cannot be generated until one competing catalog is disabled or its claim removed:",
		counted(len(contracts), "over-subscribed contract", "over-subscribed contracts"))
	for _, contract := range contracts {
		providers := slices.Sorted(slices.Values(inv.ProvidedBy[contract]))
		fmt.Fprintf(&b, "\n  %s%s provided by %s", contract, definedBy(inv, contract), strings.Join(providers, ", "))
	}
	return b.String()
}

// comparableFinding words the discrimination refusal: each comparable pair's
// broader transformer, narrower transformer and the contracts they share
// (0015:D5). No arbitration is offered because 0015:D5 takes none —
// the pair is refused, not ordered.
func comparableFinding(inv *platform.ContractInventory) string {
	rows := slices.Clone(inv.Comparable)
	slices.SortFunc(rows, func(a, b platform.ComparablePredicates) int {
		if c := strings.Compare(a.Broader, b.Broader); c != 0 {
			return c
		}
		return strings.Compare(a.Narrower, b.Narrower)
	})

	var b strings.Builder
	fmt.Fprintf(&b, "platform is not discriminated: %s; every component the narrower transformer matches is also matched by the broader one, so both would render (0015:D5):",
		counted(len(rows), "comparable transformer pair", "comparable transformer pairs"))
	for _, row := range rows {
		shared := slices.Sorted(slices.Values(row.Contracts))
		fmt.Fprintf(&b, "\n  %s (broader) and %s (narrower) over %s", row.Broader, row.Narrower, strings.Join(shared, ", "))
	}
	return b.String()
}

// setContractsFulfilled writes the non-gating 0015:D18 report on plat from the
// inventory of the package renders are consuming. It is called only where
// such a package exists — after a fresh build is recorded, and on the
// current-package skip — never on a failure or a refusal, which leave the
// condition describing the last good package for the same reason
// recordEffectiveRegistry leaves status.registry describing it.
//
// The vacuous case gets a reason of its own rather than sharing
// ContractsFulfilled: "no contract was defined" and "every defined contract
// has a provider" are different statements, and collapsing them hides the
// one 0015's operational notes warn about. It is True rather than Unknown
// because a raw-passthrough-only platform legitimately defines nothing.
func setContractsFulfilled(plat *releasesv1alpha1.Platform, inv *platform.ContractInventory) {
	switch {
	case len(inv.DefinedBy) == 0:
		conditions.MarkTrue(plat, status.ContractsFulfilledCondition, status.NoContractsDefinedReason,
			"%s", "the enabled catalogs define no contract, so nothing was verified")

	case !inv.Fulfilled:
		contracts := slices.Sorted(slices.Values(inv.Unfulfilled))
		var b strings.Builder
		fmt.Fprintf(&b, "%s defined by an enabled catalog and implemented by nothing on the platform; a module demanding one will not resolve until a provider registers:",
			counted(len(contracts), "provider-fulfilled contract is", "provider-fulfilled contracts are"))
		for _, contract := range contracts {
			fmt.Fprintf(&b, "\n  %s%s", contract, definedBy(inv, contract))
		}
		conditions.MarkFalse(plat, status.ContractsFulfilledCondition, status.UnfulfilledContractsReason, "%s", b.String())

	default:
		conditions.MarkTrue(plat, status.ContractsFulfilledCondition, status.ContractsFulfilledReason,
			"every provider-fulfilled contract the enabled catalogs define has a provider (%d defined)", len(inv.DefinedBy))
	}
}

// definedBy renders the catalog that lists contract, as the parenthetical
// every diagnostic prints beside it. DefinedBy lacks the key when no enabled
// catalog defines the contract: its defining catalog is disabled or absent
// while registry entries still provide it, which over-subscription counts all
// the same. The message then drops the parenthetical rather than printing an
// empty one.
func definedBy(inv *platform.ContractInventory, contract string) string {
	if catalog := inv.DefinedBy[contract]; catalog != "" {
		return " (defined by " + catalog + ")"
	}
	return ""
}

// counted renders a count with the noun agreeing with it: "1 contract",
// "2 contracts".
func counted(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}
