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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ModuleReleaseSpec defines the desired state of ModuleRelease
type ModuleReleaseSpec struct {
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// Module identifies the CUE module to evaluate from the OCI registry.
	Module ModuleReference `json:"module"`

	// Values contains arbitrary release input values.
	// +optional
	Values *RawValues `json:"values,omitempty"`

	// +optional
	Prune bool `json:"prune,omitempty"`

	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// +optional
	Rollout *RolloutSpec `json:"rollout,omitempty"`
}

// ModuleReleaseStatus defines the observed state of ModuleRelease.
type ModuleReleaseStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the ModuleRelease resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	LastAttemptedAction string `json:"lastAttemptedAction,omitempty"`

	// +optional
	LastAttemptedAt *metav1.Time `json:"lastAttemptedAt,omitempty"`

	// +optional
	LastAttemptedDuration *metav1.Duration `json:"lastAttemptedDuration,omitempty"`

	// +optional
	LastAttemptedSourceDigest string `json:"lastAttemptedSourceDigest,omitempty"`

	// +optional
	LastAttemptedConfigDigest string `json:"lastAttemptedConfigDigest,omitempty"`

	// +optional
	LastAttemptedRenderDigest string `json:"lastAttemptedRenderDigest,omitempty"`

	// +optional
	LastAppliedAt *metav1.Time `json:"lastAppliedAt,omitempty"`

	// +optional
	LastAppliedSourceDigest string `json:"lastAppliedSourceDigest,omitempty"`

	// +optional
	LastAppliedConfigDigest string `json:"lastAppliedConfigDigest,omitempty"`

	// +optional
	LastAppliedRenderDigest string `json:"lastAppliedRenderDigest,omitempty"`

	// +optional
	FailureCounters *FailureCounters `json:"failureCounters,omitempty"`

	// +optional
	Inventory *Inventory `json:"inventory,omitempty"`

	// +optional
	History []HistoryEntry `json:"history,omitempty"`

	// NextRetryAt indicates when the controller will next attempt reconciliation
	// after a transient or stalled failure. Nil when the resource is healthy or no-op.
	// +optional
	NextRetryAt *metav1.Time `json:"nextRetryAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mr
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Module",type=string,JSONPath=".spec.module.path"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.module.version"
// +kubebuilder:printcolumn:name="Retry",type=date,JSONPath=".status.nextRetryAt",priority=1

// ModuleRelease is the Schema for the modulereleases API
type ModuleRelease struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ModuleRelease
	// +required
	Spec ModuleReleaseSpec `json:"spec"`

	// status defines the observed state of ModuleRelease
	// +optional
	Status ModuleReleaseStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ModuleReleaseList contains a list of ModuleRelease
type ModuleReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ModuleRelease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModuleRelease{}, &ModuleReleaseList{})
}
