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
// It is a near-1:1 projection of the core #Platform definition: an
// informational type discriminator plus a path-keyed registry of catalog
// subscriptions. Its shape maps directly onto synth.PlatformInput so a later
// reconciler can convert spec to synth input without a translation layer.
type PlatformSpec struct {
	// Type is the informational discriminator for the platform (core
	// #Platform.type). It does not affect matching; it labels the platform
	// flavor for operators and downstream tooling.
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`

	// Registry is the set of catalog subscriptions keyed by catalog CUE module
	// path (e.g. "opmodel.dev/catalogs/opm"), projecting core #Platform.#registry.
	// +optional
	Registry map[string]Subscription `json:"registry,omitempty"`
}

// Subscription is a single catalog registry subscription, projecting core
// #Subscription. It maps onto synth.SubscriptionSpec.
type Subscription struct {
	// Enable toggles the subscription. A pointer so that an omitted value
	// defers to the schema default (true) rather than serializing as an
	// explicit false; matches synth.SubscriptionSpec.Enable.
	// +optional
	Enable *bool `json:"enable,omitempty"`

	// Filter optionally constrains the subscribed versions by SemVer range
	// and explicit allow/deny lists.
	// +optional
	Filter *SubscriptionFilter `json:"filter,omitempty"`
}

// SubscriptionFilter constrains subscribed catalog versions, projecting core
// #SubscriptionFilter. It maps onto synth.FilterSpec.
type SubscriptionFilter struct {
	// Range is a SemVer constraint (e.g. ">=1.2.0 <2.0.0").
	// +optional
	Range string `json:"range,omitempty"`

	// Allow is an explicit allowlist of versions.
	// +optional
	Allow []string `json:"allow,omitempty"`

	// Deny is an explicit denylist of versions.
	// +optional
	Deny []string `json:"deny,omitempty"`
}

// PlatformStatus defines the observed state of Platform.
type PlatformStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the Platform resource. The
	// PlatformReconciler summarizes materialization on the Ready condition:
	// Ready=True (reason Materialized) on success, Ready=False (reason
	// MaterializeFailed) on a MaterializeError.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=plat
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="Platform is a cluster singleton; the only permitted name is 'cluster'"
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].reason"

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
