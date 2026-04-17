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
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReleaseSpec defines the desired state of Release.
// A Release points to a Flux source artifact containing a CUE release
// package. The controller fetches the artifact, navigates to spec.path,
// loads release.cue, detects whether it evaluates to #ModuleRelease or
// #BundleRelease, and dispatches to the appropriate render pipeline.
type ReleaseSpec struct {
	// SourceRef references a Flux source (OCIRepository, GitRepository, or Bucket)
	// that provides the artifact containing the CUE release package.
	SourceRef SourceReference `json:"sourceRef"`

	// Path is the directory within the artifact containing release.cue.
	// Example: "releases/prod/minecraft".
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`

	// Interval at which the reconciler re-evaluates the release to detect drift
	// and re-apply. Also the requeue interval after transient failures.
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`

	// DependsOn references other Release CRs that must be Ready=True before
	// this Release is reconciled. References are same-namespace only.
	// +optional
	DependsOn []fluxmeta.NamespacedObjectReference `json:"dependsOn,omitempty"`

	// Prune enables deletion of stale resources on reconcile and of all owned
	// resources on Release deletion.
	// +optional
	Prune bool `json:"prune,omitempty"`

	// Suspend halts reconciliation when true.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount used to impersonate
	// during apply and prune. Empty means use the controller's identity.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Rollout configures apply behavior.
	// +optional
	Rollout *RolloutSpec `json:"rollout,omitempty"`
}

// ReleaseStatus defines the observed state of Release.
type ReleaseStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the Release resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Source is the resolved Flux source artifact metadata.
	// +optional
	Source *SourceStatus `json:"source,omitempty"`

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
	// after a transient or stalled failure.
	// +optional
	NextRetryAt *metav1.Time `json:"nextRetryAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=rel
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=".spec.sourceRef.name"
// +kubebuilder:printcolumn:name="Path",type=string,JSONPath=".spec.path"
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=".status.source.artifactRevision",priority=1
// +kubebuilder:printcolumn:name="Retry",type=date,JSONPath=".status.nextRetryAt",priority=1

// Release is the Schema for the releases API.
type Release struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Release
	// +required
	Spec ReleaseSpec `json:"spec"`

	// status defines the observed state of Release
	// +optional
	Status ReleaseStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ReleaseList contains a list of Release.
type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Release `json:"items"`
}

// GetConditions returns the status conditions of the Release.
func (in *Release) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the Release.
func (in *Release) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&Release{}, &ReleaseList{})
}
