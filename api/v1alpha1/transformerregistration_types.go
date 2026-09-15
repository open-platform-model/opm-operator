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

// TransformerRegistrationSpec defines a provider module's claim that its
// catalog implements platform contracts (enhancement 0015 D3).
//
// Every field is required at the CRD level, deliberately duplicating the
// catalog-side contract rather than trusting it. CUE reports a missing
// required field as an incomplete value, not an error: measured 2026-09-14 at
// cue v0.17.1, a component omitting `catalog` passes `cue vet ./...` and fails
// only under `cue export`. A claim can therefore reach the cluster with a
// field absent, so the API server validates it on its own terms.
type TransformerRegistrationSpec struct {
	// Catalog is the provider catalog's major-suffixed CUE module path
	// (e.g. "opmodel.dev/catalogs/k8up@v1"). It names a catalog, never a
	// module: a catalog is what carries transformers, so a claim naming a
	// module names nothing that could implement a contract. Acceptance
	// refuses a non-catalog artifact structurally (enhancement 0015 D10).
	// +kubebuilder:validation:MinLength=1
	// +required
	Catalog string `json:"catalog"`

	// Version is the released build of the named catalog, a bare SemVer
	// string (e.g. "1.0.0"). It pins the claim to one build, which is what
	// lets acceptance re-derive Provides from the artifact this names.
	// +kubebuilder:validation:MinLength=1
	// +required
	Version string `json:"version"`

	// Provides lists every provider-fulfilled contract FQN the named catalog
	// claims to implement.
	//
	// Required but MAY be empty. A provider catalog implementing no
	// provider-fulfilled contract is a claim acceptance refuses on its merits,
	// naming the catalog it re-derived from, not a malformed object. The CRD
	// is the wrong place to encode a rule the reconciler states better.
	// +required
	Provides []string `json:"provides"`

	// ProviderRef identifies the ModuleInstance that rendered this claim. It
	// is stamped from the rendering instance and never authored, so a module
	// cannot claim to be another provider (enhancement 0015 D11).
	// +required
	ProviderRef ProviderReference `json:"providerRef"`
}

// TransformerRegistrationStatus defines the observed state of a
// TransformerRegistration.
//
// Acceptance and activation are separate states (enhancement 0015 D3): a
// stored claim is not yet judged, accepted but inactive, or active. The
// acceptance reconciler writes conditions, accepted and observedGeneration;
// active stays false until an accepted claim's provider is serving.
type TransformerRegistrationStatus struct {
	// observedGeneration is the .metadata.generation this claim was last
	// reconciled for, whether or not that reconcile reached a verdict: a claim
	// waiting on the platform or on its provider's inventory records the
	// generation it observed rather than reading as un-reconciled. A claim
	// whose generation is ahead of this has been edited since, so whatever
	// conditions report is stale.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the TransformerRegistration
	// resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// accepted reports whether the claim passed acceptance: the catalog
	// resolved, its Provides re-derived equal, and no other instance holds
	// the same provider.
	// +optional
	Accepted bool `json:"accepted,omitzero"`

	// active reports whether an accepted claim's provider is serving. An
	// accepted claim stays inactive until its ModulePackage is Ready.
	// +optional
	Active bool `json:"active,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=treg
// +kubebuilder:validation:XValidation:rule="self.metadata.name.contains('.')",message="name must be the dot-joined <namespace>.<name> of the claiming instance"
// +kubebuilder:printcolumn:name="Catalog",type=string,JSONPath=".spec.catalog"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=".status.accepted"
// +kubebuilder:printcolumn:name="Active",type=string,JSONPath=".status.active"

// TransformerRegistration is the Schema for the transformerregistrations API.
// It is the cluster-scoped claim a provider module ships among its rendered
// resources, the second path by which transformers reach a platform
// (enhancement 0015 D3); the first is a subscription in Platform.spec.registry.
//
// metadata.name is the dot-joined "<namespace>.<name>" of the claiming
// instance (enhancement 0015 D12). A namespace cannot contain a dot, so the
// join is collision-free and two instances of one provider module produce two
// distinct claims, the second refused at acceptance naming the claimant,
// rather than contending for one object.
//
// Creating one requires platform-admin RBAC. The operator ships that role
// unbound (config/rbac/transformerregistration_admin_role.yaml) and ships no
// tenant-facing role granting create, so a module applied under an
// impersonated tenant ServiceAccount cannot register a transformer unless a
// cluster administrator deliberately bound it.
type TransformerRegistration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TransformerRegistration
	// +required
	Spec TransformerRegistrationSpec `json:"spec"`

	// status defines the observed state of TransformerRegistration
	// +optional
	Status TransformerRegistrationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TransformerRegistrationList contains a list of TransformerRegistration.
type TransformerRegistrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TransformerRegistration `json:"items"`
}

// GetConditions returns the status conditions of the TransformerRegistration.
func (in *TransformerRegistration) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the TransformerRegistration.
func (in *TransformerRegistration) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(GroupVersion, &TransformerRegistration{}, &TransformerRegistrationList{})
		return nil
	})
}
