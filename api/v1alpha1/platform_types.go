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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PlatformSpec defines the desired state of Platform.
// It is a near-1:1 projection of the core #Platform author surface: an
// informational type discriminator plus a path-keyed registry of catalog
// subscriptions. The operator generates a platform CUE module from it on its
// own disk (a cue.mod pinning every subscribed catalog and a platform.cue
// carrying each catalog by import) and builds that module; the CR stays the
// API and the module is derived state (0019:D6).
type PlatformSpec struct {
	// Type is the informational discriminator for the platform (core
	// #Platform.type). It does not affect matching; it labels the platform
	// flavor for operators and downstream tooling.
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`

	// Registry is the set of catalog subscriptions keyed by major-suffixed
	// catalog CUE module path (e.g. "opmodel.dev/catalogs/opm@v2"), projecting
	// core #Platform.#registry. The key's major must agree with the major of
	// the subscribed version (0010:D14).
	// +optional
	Registry map[string]Subscription `json:"registry,omitempty"`

	// SkewPolicy is the operator's response to catalog version skew: a
	// module whose cue.mod requires a newer build of an OPM-namespace path
	// (core or a catalog) than the platform pins (0019:D7/D18).
	// "Warn" (the default when unset) renders against the platform's build
	// and reports the skew as a RenderWarning event on the workload; "Refuse"
	// refuses the render before evaluation and the workload reports
	// Ready=False with reason SkewRefused, naming the path and both versions.
	// The policy is not part of the generated platform module; it is recorded
	// beside it, so changing the field alone bumps the generation, regenerates
	// and re-enqueues the workloads.
	// +kubebuilder:validation:Enum=Warn;Refuse
	// +optional
	SkewPolicy *string `json:"skewPolicy,omitempty"`
}

// Skew policy values for PlatformSpec.SkewPolicy.
const (
	// SkewPolicyWarn renders against the platform's build and reports the
	// skew. The default.
	SkewPolicyWarn = "Warn"

	// SkewPolicyRefuse refuses the render before evaluation.
	SkewPolicyRefuse = "Refuse"
)

// Subscription is a single catalog registry subscription. It becomes one
// #registry entry of the generated platform module, carrying the catalog by
// import.
type Subscription struct {
	// Enable toggles the subscription. A pointer so that an omitted value
	// defers to the schema default (true) rather than serializing as an
	// explicit false. A disabled subscription is still pinned and imported by
	// the generated module, with enable set to false on its entry.
	// +optional
	Enable *bool `json:"enable,omitempty"`

	// Version names exactly one published catalog build as a bare SemVer
	// string (e.g. "2.0.0-alpha.3") — the platform module IS the resolution
	// (0010:D14); there is no range or allow/deny vocabulary. The
	// version's major must agree with the subscription key's `@vN` suffix.
	// The operator uses it twice: as the generated cue.mod pin and as the
	// entry's stamped expected version, which unifies with the imported
	// catalog's own version so wrong bytes fail the build naming the entry
	// (0019:D13).
	// CRD-required is safe against the stored pre-reshape singleton: API
	// server validation ratcheting keeps status-subresource patches working
	// against a stored object lacking the field (measured in
	// test/integration/crdvalidation).
	// +kubebuilder:validation:MinLength=1
	// +required
	Version string `json:"version"`
}

// PlatformStatus defines the observed state of Platform. RegistryEntrySource
// names how a catalog reached the platform's resolved registry: the two paths
// transformers take to a platform (0015:D3).
// +kubebuilder:validation:Enum=Subscription;Registration
type RegistryEntrySource string

const (
	// RegistryEntrySubscription is a catalog the Platform CR's spec.registry
	// authored: a platform admin subscribed to it deliberately.
	RegistryEntrySubscription RegistryEntrySource = "Subscription"

	// RegistryEntryRegistration is a catalog an accepted-and-active
	// TransformerRegistration contributed: a provider module registered it
	// and the operator judged the claim.
	RegistryEntryRegistration RegistryEntrySource = "Registration"
)

// ResolvedRegistryEntry is one catalog of the registry the platform is
// actually running, resolved from both of its sources. A catalog is one
// registry key, so an authored subscription and a claim naming the same
// catalog resolve to a single entry sourced Subscription: the admin's pin
// and enable decision is the deliberate one.
type ResolvedRegistryEntry struct {
	// catalog is the major-suffixed CUE module path of the catalog.
	// +kubebuilder:validation:MinLength=1
	// +required
	Catalog string `json:"catalog"`

	// version is the bare SemVer build the generated platform module pins
	// the catalog at.
	// +kubebuilder:validation:MinLength=1
	// +required
	Version string `json:"version"`

	// enabled reports whether the catalog's transformers register. A
	// disabled subscription is still pinned and imported by the generated
	// module, so it belongs in the resolved registry, but it contributes no
	// transformer; reading the list without this field would overstate what
	// the platform runs. Every catalog an active claim contributed is
	// enabled.
	// +optional
	Enabled bool `json:"enabled,omitzero"`

	// source records which of the two transformer paths put the catalog
	// here, so an operator can tell an authored subscription from one a
	// provider registered.
	// +required
	Source RegistryEntrySource `json:"source"`
}

type PlatformStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// packageIdentity identifies the generated platform package by the two
	// inputs it is a function of (0015:D13): this CR's
	// generation and the sorted set of active claims' catalog coordinates.
	// The same inputs always produce the same value and any change to either
	// produces a different one.
	//
	// This, not status.active on a TransformerRegistration, is the
	// authoritative answer to what a render is building against. The claim
	// reconciler writes the verdict on the claim and this reconciler folds
	// the verdict into the package, so the two are eventually consistent: an
	// observer can see a claim active for a moment before a package
	// containing its catalog exists. Generation is level-computed, so the
	// next regeneration catches up; until it lands, an active claim is a
	// promise about the next package, not a statement about the current one.
	// +optional
	PackageIdentity string `json:"packageIdentity,omitempty"`

	// registry is the resolved union of the registry the platform is
	// running: the subscriptions spec.registry authored plus the catalogs
	// active TransformerRegistrations contributed, each entry recording
	// which. It makes the effective set enumerable from the Platform rather
	// than only by listing every claim, and it is rewritten on every
	// generation, so it follows the active set in both directions.
	// +listType=map
	// +listMapKey=catalog
	// +optional
	Registry []ResolvedRegistryEntry `json:"registry,omitempty"`

	// conditions represent the current state of the Platform resource. The
	// PlatformReconciler summarizes module generation on the Ready condition:
	// Ready=True (reason Generated) when the generated platform module built
	// and its contract inventory was accepted, Ready=False with reason
	// BuildFailed (a pinned build does not exist, a key disagrees with its
	// imported catalog, the module did not build, or its contract inventory
	// could not be read; the message names the dependency, the entry or the
	// field), GenerateFailed (the module could not be written to the
	// operator's disk), OverSubscribedContracts (the module built, but a
	// provider-fulfilled contract is required by transformers from more than
	// one enabled catalog, so the platform cannot route it; the message names
	// each contract, its defining catalog and the requiring transformers) or
	// ComparablePredicates (the module built, but two enabled transformers
	// have comparable match predicates over a shared catalog-fulfilled
	// contract, so both would render every component the narrower matches;
	// the message names each pair and the contracts it shares). A refused
	// module is not recorded: the last good package keeps serving renders.
	//
	// The ContractsFulfilled condition is a separate, non-gating report on
	// the package renders are consuming, written whenever one is recorded:
	// False with reason UnfulfilledContracts when the package defines
	// provider-fulfilled contracts nothing on the platform implements (the
	// message names each and its defining catalog), True with reason
	// ContractsFulfilled when every defined contract has a provider, and True
	// with reason NoContractsDefined when the enabled catalogs list no
	// contract at all, so nothing was verified. It never moves Ready.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// operatorVersion is the version of the operator that last patched this
	// Platform's status, stamped on every reconcile regardless of outcome
	// (0006:D24). The CLI reads it as the version-skew ceiling;
	// absence means no operator has reconciled the Platform (solo cluster).
	// +optional
	OperatorVersion string `json:"operatorVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=plat
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="Platform is a cluster singleton; the only permitted name is 'cluster'"
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].reason"
// +kubebuilder:printcolumn:name="Operator",type=string,JSONPath=".status.operatorVersion"

// Platform is the Schema for the platforms API. It is a cluster-scoped
// singleton (the only permitted name is "cluster") whose spec projects the
// core #Platform author surface.
type Platform struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Platform
	// +required
	Spec PlatformSpec `json:"spec"`

	// status defines the observed state of Platform
	// +optional
	Status PlatformStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PlatformList contains a list of Platform.
type PlatformList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Platform `json:"items"`
}

// GetConditions returns the status conditions of the Platform.
func (in *Platform) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the Platform.
func (in *Platform) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(GroupVersion, &Platform{}, &PlatformList{})
		return nil
	})
}
