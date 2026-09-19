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
	"strings"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/open-platform-model/library/opm/platform"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// The FQNs below model the shapes the refusals name; no published catalog
// pair produces either (the refusals are tested without a
// refusing catalog pair), so the inventories are hand-built.
// backupTrait and restoreTrait are the package's existing contract FQNs
// (transformerregistration_catalog_test.go).
const (
	opmCatalog    = "opmodel.dev/catalogs/opm@v4"
	veleroCatalog = "opmodel.dev/catalogs/velero@v1"

	k8upSchedule   = "opmodel.dev/catalogs/k8up/transformers/schedule@1.0.0"
	veleroSchedule = "opmodel.dev/catalogs/velero/transformers/schedule@1.0.0"

	containerResource = "testing.opmodel.dev/cat/resources/container@v1"
	volumeResource    = "testing.opmodel.dev/cat/resources/volume@v1"
	mirrorTransformer = "testing.opmodel.dev/cat2/transformers/mirror@0.2.0"
	deployTransformer = "testing.opmodel.dev/cat/transformers/deployment@0.1.0"
)

func TestInventoryRefusal(t *testing.T) {
	tests := []struct {
		name string
		inv  *platform.ContractInventory

		wantRefused bool
		wantReason  string
		wantMessage string
	}{
		{
			// Several transformers sharing one contract is the ordinary
			// case, not a refusal: discrimination is about predicates
			// being comparable, never about how many transformers a
			// contract has.
			name: "routable and discriminated is not refused, however many transformers share a contract",
			inv: &platform.ContractInventory{
				DefinedBy: map[string]string{backupTrait: opmCatalog},
				RequiredBy: map[string][]string{
					backupTrait: {k8upSchedule, mirrorTransformer, deployTransformer},
				},
				Fulfilled:     true,
				Routable:      true,
				Discriminated: true,
			},
			wantRefused: false,
		},
		{
			name: "over-subscribed only names the contract, its catalog and every requiring transformer",
			inv: &platform.ContractInventory{
				DefinedBy:  map[string]string{backupTrait: opmCatalog},
				RequiredBy: map[string][]string{backupTrait: {veleroSchedule, k8upSchedule}},
				// Core lists in comprehension order; the message sorts.
				OverSubscribed: []string{backupTrait},
				Routable:       false,
				Discriminated:  true,
			},
			wantRefused: true,
			wantReason:  status.OverSubscribedContractsReason,
			wantMessage: "platform is not routable: 1 over-subscribed contract; " +
				"a platform package cannot be generated until one competing catalog is disabled or its claim removed:" +
				"\n  " + backupTrait + " (defined by " + opmCatalog + ") required by " + k8upSchedule + ", " + veleroSchedule,
		},
		{
			name: "two over-subscribed contracts are listed in sorted order with an agreeing count",
			inv: &platform.ContractInventory{
				DefinedBy: map[string]string{
					restoreTrait: veleroCatalog,
					backupTrait:  opmCatalog,
				},
				RequiredBy: map[string][]string{
					restoreTrait: {veleroSchedule, k8upSchedule},
					backupTrait:  {veleroSchedule, k8upSchedule},
				},
				OverSubscribed: []string{restoreTrait, backupTrait},
				Routable:       false,
				Discriminated:  true,
			},
			wantRefused: true,
			wantReason:  status.OverSubscribedContractsReason,
			wantMessage: "platform is not routable: 2 over-subscribed contracts; " +
				"a platform package cannot be generated until one competing catalog is disabled or its claim removed:" +
				"\n  " + backupTrait + " (defined by " + opmCatalog + ") required by " + k8upSchedule + ", " + veleroSchedule +
				"\n  " + restoreTrait + " (defined by " + veleroCatalog + ") required by " + k8upSchedule + ", " + veleroSchedule,
		},
		{
			name: "a comparable pair names broader, narrower and the shared contracts",
			inv: &platform.ContractInventory{
				DefinedBy:  map[string]string{containerResource: opmCatalog},
				RequiredBy: map[string][]string{containerResource: {mirrorTransformer, deployTransformer}},
				Comparable: []platform.ComparablePredicates{
					{Broader: mirrorTransformer, Narrower: deployTransformer, Contracts: []string{volumeResource, containerResource}},
				},
				Routable:      true,
				Discriminated: false,
			},
			wantRefused: true,
			wantReason:  status.ComparablePredicatesReason,
			wantMessage: "platform is not discriminated: 1 comparable transformer pair; " +
				"every component the narrower transformer matches is also matched by the broader one, so both would render (0015:D5):" +
				"\n  " + mirrorTransformer + " (broader) and " + deployTransformer + " (narrower) over " + containerResource + ", " + volumeResource,
		},
		{
			name: "both refusals report under the routing reason and carry both findings",
			inv: &platform.ContractInventory{
				DefinedBy: map[string]string{
					backupTrait:       opmCatalog,
					containerResource: opmCatalog,
				},
				RequiredBy: map[string][]string{
					backupTrait:       {veleroSchedule, k8upSchedule},
					containerResource: {mirrorTransformer, deployTransformer},
				},
				OverSubscribed: []string{backupTrait},
				Comparable: []platform.ComparablePredicates{
					{Broader: mirrorTransformer, Narrower: deployTransformer, Contracts: []string{containerResource}},
				},
				Routable:      false,
				Discriminated: false,
			},
			wantRefused: true,
			wantReason:  status.OverSubscribedContractsReason,
			wantMessage: "platform is not routable: 1 over-subscribed contract; " +
				"a platform package cannot be generated until one competing catalog is disabled or its claim removed:" +
				"\n  " + backupTrait + " (defined by " + opmCatalog + ") required by " + k8upSchedule + ", " + veleroSchedule +
				"\n\n" +
				"platform is not discriminated: 1 comparable transformer pair; " +
				"every component the narrower transformer matches is also matched by the broader one, so both would render (0015:D5):" +
				"\n  " + mirrorTransformer + " (broader) and " + deployTransformer + " (narrower) over " + containerResource,
		},
		{
			name: "a contract with no DefinedBy entry drops the parenthetical rather than printing an empty one",
			inv: &platform.ContractInventory{
				DefinedBy:      map[string]string{},
				RequiredBy:     map[string][]string{backupTrait: {k8upSchedule, veleroSchedule}},
				OverSubscribed: []string{backupTrait},
				Routable:       false,
				Discriminated:  true,
			},
			wantRefused: true,
			wantReason:  status.OverSubscribedContractsReason,
			wantMessage: "platform is not routable: 1 over-subscribed contract; " +
				"a platform package cannot be generated until one competing catalog is disabled or its claim removed:" +
				"\n  " + backupTrait + " required by " + k8upSchedule + ", " + veleroSchedule,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, msg, refused := inventoryRefusal(tt.inv)
			if refused != tt.wantRefused {
				t.Fatalf("refused = %t, want %t (reason %q, message %q)", refused, tt.wantRefused, reason, msg)
			}
			if !tt.wantRefused {
				if reason != "" || msg != "" {
					t.Fatalf("an accepted inventory must return zero values, got reason %q message %q", reason, msg)
				}
				return
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
			if msg != tt.wantMessage {
				t.Errorf("message mismatch\n got: %q\nwant: %q", msg, tt.wantMessage)
			}
		})
	}
}

// TestInventoryUnreadableIsAnErrorNotAnEmptyInventory pins the library
// contract the reconciler's BuildFailed branch rests on
// (platform_controller.go, the gate): a platform carrying no readable
// #contracts must come back as an error, never as a zero-value inventory.
//
// The distinction is the whole of "never a silent pass". A zero-value
// ContractInventory has Routable and Discriminated false, so it would refuse
// rather than pass — but with an empty OverSubscribed list, producing a
// refusal message naming nothing. If the library ever softened this to a
// partial inventory, that is what an operator would see, and this test is
// what catches it.
//
// The reconciler's own branch is not exercised end to end: reaching it needs
// a built platform whose core predates the library's pin, which the library's
// generator cannot produce, and stubbing the build seam is the seam-whose-
// only-caller-is-a-test that the refusal-testing rule rejected. The change's
// own risk list records the accepted gap.
func TestInventoryUnreadableIsAnErrorNotAnEmptyInventory(t *testing.T) {
	inv, err := (&platform.Platform{}).Contracts()

	if err == nil {
		t.Fatalf("a platform with no #contracts must not read as an inventory, got %+v", inv)
	}
	if inv != nil {
		t.Errorf("a failed read must return no inventory, got %+v", inv)
	}
	if !strings.Contains(err.Error(), "contracts") {
		t.Errorf("the error must name the field the read failed on, got %q", err)
	}
}

// TestInventoryRefusal_MessageIsOrderIndependent is the event-gating
// guarantee: failReconcile emits its warning event only when the message
// changes, so two builds reporting the same problem in different
// comprehension orders must produce one message, not two.
func TestInventoryRefusal_MessageIsOrderIndependent(t *testing.T) {
	definedByCatalog := map[string]string{
		backupTrait:       opmCatalog,
		restoreTrait:      veleroCatalog,
		containerResource: opmCatalog,
	}
	requiredBy := map[string][]string{
		backupTrait:       {veleroSchedule, k8upSchedule},
		restoreTrait:      {k8upSchedule, veleroSchedule},
		containerResource: {mirrorTransformer, deployTransformer},
	}

	one := &platform.ContractInventory{
		DefinedBy:      definedByCatalog,
		RequiredBy:     requiredBy,
		OverSubscribed: []string{backupTrait, restoreTrait},
		Comparable: []platform.ComparablePredicates{
			{Broader: mirrorTransformer, Narrower: deployTransformer, Contracts: []string{containerResource, volumeResource}},
			{Broader: deployTransformer, Narrower: mirrorTransformer, Contracts: []string{volumeResource}},
		},
		Routable:      false,
		Discriminated: false,
	}
	other := &platform.ContractInventory{
		DefinedBy: definedByCatalog,
		RequiredBy: map[string][]string{
			backupTrait:       {k8upSchedule, veleroSchedule},
			restoreTrait:      {veleroSchedule, k8upSchedule},
			containerResource: {deployTransformer, mirrorTransformer},
		},
		OverSubscribed: []string{restoreTrait, backupTrait},
		Comparable: []platform.ComparablePredicates{
			{Broader: deployTransformer, Narrower: mirrorTransformer, Contracts: []string{volumeResource}},
			{Broader: mirrorTransformer, Narrower: deployTransformer, Contracts: []string{volumeResource, containerResource}},
		},
		Routable:      false,
		Discriminated: false,
	}

	reasonOne, msgOne, refusedOne := inventoryRefusal(one)
	reasonOther, msgOther, refusedOther := inventoryRefusal(other)

	if !refusedOne || !refusedOther {
		t.Fatalf("both orderings must refuse, got %t and %t", refusedOne, refusedOther)
	}
	if reasonOne != reasonOther {
		t.Errorf("reason differs across orderings: %q vs %q", reasonOne, reasonOther)
	}
	if msgOne != msgOther {
		t.Errorf("message differs across orderings, so an unchanged verdict would re-fire the warning event\n one: %q\nother: %q", msgOne, msgOther)
	}
}

// TestInventoryRefusal_DoesNotMutateTheInventory guards the sorting: the
// inventory belongs to the built platform the store holds, and the skip path
// reads it again on the next reconcile.
func TestInventoryRefusal_DoesNotMutateTheInventory(t *testing.T) {
	inv := &platform.ContractInventory{
		DefinedBy:      map[string]string{backupTrait: opmCatalog, restoreTrait: veleroCatalog},
		RequiredBy:     map[string][]string{backupTrait: {veleroSchedule, k8upSchedule}},
		OverSubscribed: []string{restoreTrait, backupTrait},
		Comparable: []platform.ComparablePredicates{
			{Broader: mirrorTransformer, Narrower: deployTransformer, Contracts: []string{volumeResource, containerResource}},
			{Broader: deployTransformer, Narrower: mirrorTransformer, Contracts: []string{volumeResource}},
		},
		Routable:      false,
		Discriminated: false,
	}

	if _, _, refused := inventoryRefusal(inv); !refused {
		t.Fatal("expected the inventory to be refused")
	}

	if got := inv.OverSubscribed; got[0] != restoreTrait || got[1] != backupTrait {
		t.Errorf("OverSubscribed was reordered in place: %v", got)
	}
	if got := inv.RequiredBy[backupTrait]; got[0] != veleroSchedule || got[1] != k8upSchedule {
		t.Errorf("RequiredBy was reordered in place: %v", got)
	}
	if got := inv.Comparable; got[0].Broader != mirrorTransformer || got[1].Broader != deployTransformer {
		t.Errorf("Comparable was reordered in place: %v", got)
	}
	if got := inv.Comparable[0].Contracts; got[0] != volumeResource || got[1] != containerResource {
		t.Errorf("Comparable[0].Contracts was reordered in place: %v", got)
	}
}

func TestInventoryContractsFulfilled(t *testing.T) {
	tests := []struct {
		name string
		inv  *platform.ContractInventory

		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{
			name: "no contract defined is visible, not a pass",
			inv: &platform.ContractInventory{
				DefinedBy:     map[string]string{},
				RequiredBy:    map[string][]string{},
				Fulfilled:     true,
				Routable:      true,
				Discriminated: true,
			},
			wantStatus:  metav1.ConditionTrue,
			wantReason:  status.NoContractsDefinedReason,
			wantMessage: "the enabled catalogs define no contract, so nothing was verified",
		},
		{
			name: "every defined contract has a provider",
			inv: &platform.ContractInventory{
				DefinedBy:     map[string]string{backupTrait: opmCatalog, restoreTrait: veleroCatalog},
				RequiredBy:    map[string][]string{backupTrait: {k8upSchedule}, restoreTrait: {veleroSchedule}},
				Fulfilled:     true,
				Routable:      true,
				Discriminated: true,
			},
			wantStatus:  metav1.ConditionTrue,
			wantReason:  status.ContractsFulfilledReason,
			wantMessage: "every provider-fulfilled contract the enabled catalogs define has a provider (2 defined)",
		},
		{
			name: "one unfulfilled contract names it and its defining catalog",
			inv: &platform.ContractInventory{
				DefinedBy:     map[string]string{backupTrait: opmCatalog},
				RequiredBy:    map[string][]string{backupTrait: {}},
				Unfulfilled:   []string{backupTrait},
				Fulfilled:     false,
				Routable:      true,
				Discriminated: true,
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: status.UnfulfilledContractsReason,
			wantMessage: "1 provider-fulfilled contract is defined by an enabled catalog and implemented by nothing on the platform; " +
				"a module demanding one will not resolve until a provider registers:" +
				"\n  " + backupTrait + " (defined by " + opmCatalog + ")",
		},
		{
			name: "two unfulfilled contracts are sorted and the count agrees",
			inv: &platform.ContractInventory{
				DefinedBy:     map[string]string{backupTrait: opmCatalog, restoreTrait: veleroCatalog},
				RequiredBy:    map[string][]string{backupTrait: {}, restoreTrait: {}},
				Unfulfilled:   []string{restoreTrait, backupTrait},
				Fulfilled:     false,
				Routable:      true,
				Discriminated: true,
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: status.UnfulfilledContractsReason,
			wantMessage: "2 provider-fulfilled contracts are defined by an enabled catalog and implemented by nothing on the platform; " +
				"a module demanding one will not resolve until a provider registers:" +
				"\n  " + backupTrait + " (defined by " + opmCatalog + ")" +
				"\n  " + restoreTrait + " (defined by " + veleroCatalog + ")",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plat := &releasesv1alpha1.Platform{}
			setContractsFulfilled(plat, tt.inv)

			cond := apimeta.FindStatusCondition(plat.Status.Conditions, status.ContractsFulfilledCondition)
			if cond == nil {
				t.Fatalf("no %s condition was written", status.ContractsFulfilledCondition)
			}
			if cond.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", cond.Status, tt.wantStatus)
			}
			if cond.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", cond.Reason, tt.wantReason)
			}
			if cond.Message != tt.wantMessage {
				t.Errorf("message mismatch\n got: %q\nwant: %q", cond.Message, tt.wantMessage)
			}
		})
	}
}

// TestInventoryContractsFulfilled_NeverTouchesReady is the 0015:D18 guarantee in
// its narrowest form: the report is not a gate.
func TestInventoryContractsFulfilled_NeverTouchesReady(t *testing.T) {
	plat := &releasesv1alpha1.Platform{}
	status.MarkReadyWithReason(plat, status.GeneratedReason, "Platform module generated and built for generation %d", 1)

	setContractsFulfilled(plat, &platform.ContractInventory{
		DefinedBy:   map[string]string{backupTrait: opmCatalog},
		RequiredBy:  map[string][]string{backupTrait: {}},
		Unfulfilled: []string{backupTrait},
	})

	ready := apimeta.FindStatusCondition(plat.Status.Conditions, status.ReadyCondition)
	if ready == nil {
		t.Fatal("Ready condition disappeared")
	}
	if ready.Status != metav1.ConditionTrue || ready.Reason != status.GeneratedReason {
		t.Errorf("an unfulfilled contract moved Ready to %s/%s", ready.Status, ready.Reason)
	}
}
