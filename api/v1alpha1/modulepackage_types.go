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
	"k8s.io/apimachinery/pkg/runtime"
)

// ModulePackageSpec defines the desired state of ModulePackage.
// A ModulePackage points to a Flux source artifact containing a CUE package.
// The controller fetches the artifact, navigates to spec.path,
// loads instance.cue, and renders it when it evaluates to #ModuleInstance;
// any other kind is rejected as unsupported.
type ModulePackageSpec struct {
	// SourceRef references a Flux source (OCIRepository, GitRepository, or Bucket)
	// that provides the artifact containing the CUE package.
	SourceRef SourceReference `json:"sourceRef"`

	// Path is the directory within the artifact containing instance.cue.
	// Example: "releases/prod/minecraft".
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`

	// Interval at which the reconciler runs again: it re-fetches the source
	// artifact, re-renders the package and compares the source, render and
	// inventory digests with the last applied ones, and skips the apply when
	// they all match. It does not compare the live objects, so a change made
	// directly to an applied object is not detected or reverted until the
	// rendered output changes. Also the requeue interval after transient
	// failures. Defaults to 5 minutes.
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`

	// DependsOn references other ModulePackage CRs that must be Ready=True before
	// this ModulePackage is reconciled. References are same-namespace only: an
	// entry whose namespace is set to anything other than this ModulePackage's
	// own is refused: the ModulePackage reports Ready=False with reason
	// DependenciesNotReady and does not reconcile.
	// +optional
	DependsOn []fluxmeta.NamespacedObjectReference `json:"dependsOn,omitempty"`

	// Prune enables deletion of stale resources on reconcile and of all owned
	// resources on ModulePackage deletion. Namespaces and
	// CustomResourceDefinitions are never deleted, and PersistentVolumeClaims
	// are kept unless spec.dataPolicy is Delete.
	// +optional
	Prune bool `json:"prune,omitempty"`

	// DataPolicy says what the operator does with the PersistentVolumeClaims it
	// would otherwise delete under spec.prune. With Keep, or when the field is
	// absent, the operator never deletes a PersistentVolumeClaim: a claim that
	// a new render no longer produces stays in the cluster and is no longer
	// tracked, and deleting the ModulePackage leaves its claims in place. With
	// Delete, claims are pruned and deleted like any other object, and the data
	// on their volumes goes with them under the reclaim policy of the volume.
	// The field has no effect unless spec.prune is true: Delete without
	// spec.prune is accepted and deletes nothing. Claims that a StatefulSet
	// creates from its volumeClaimTemplates are never tracked and never
	// deleted by the operator, whatever this field says.
	// +kubebuilder:validation:Enum=Keep;Delete
	// +optional
	DataPolicy DataPolicy `json:"dataPolicy,omitempty"`

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

// ModulePackageStatus defines the observed state of ModulePackage.
type ModulePackageStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the ModulePackage resource.
	// Ready reports whether the operator applied the last render. Healthy
	// reports whether the applied objects have rolled out: True once every
	// object in the inventory reports ready, False while one is still rolling
	// out, missing, or stalled past its progress deadline, and Unknown when
	// an object cannot be read. The two are independent, and nothing that
	// waits on Ready waits on Healthy.
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

	// lastAppliedVersion is the version of the module the operator last
	// applied, in plain text, as the module declares it in metadata.version
	// (bare SemVer, for example 0.1.0). It is set with the other last-applied
	// fields when an apply succeeds, and rewritten by a reconcile that renders
	// and finds nothing to change. It is empty when the module's version could
	// not be read.
	// +optional
	LastAppliedVersion string `json:"lastAppliedVersion,omitempty"`

	// lastAppliedInputs identifies the inputs of the last render that left
	// the cluster holding its output: a digest over the module source, the
	// values, the platform package identity, the catalog skew policy, and the
	// operator and library versions, and renderedAt, when that render ran. It
	// is set with the other last-applied fields when an apply succeeds, and
	// rewritten by a reconcile that renders and finds nothing to change.
	//
	// While the digest matches the current inputs and renderedAt is younger
	// than the manager's --drift-render-interval, a reconcile of a Ready
	// object that has observed its generation does not render, so drift is
	// re-evaluated at most once per interval while nothing changes.
	// +optional
	LastAppliedInputs *RenderInputs `json:"lastAppliedInputs,omitempty"`

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
// +kubebuilder:resource:scope=Namespaced,shortName=mpkg
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Healthy",type=string,JSONPath=".status.conditions[?(@.type=='Healthy')].status"
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=".spec.sourceRef.name"
// +kubebuilder:printcolumn:name="Path",type=string,JSONPath=".spec.path"
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=".status.source.artifactRevision",priority=1
// +kubebuilder:printcolumn:name="Retry",type=date,JSONPath=".status.nextRetryAt",priority=1

// ModulePackage renders the ModuleInstance package that a Flux source
// artifact carries.
//
// The operator fetches the artifact spec.sourceRef names, loads instance.cue
// from the directory at spec.path, and renders and applies it when it
// evaluates to #ModuleInstance; any other kind is rejected as unsupported.
type ModulePackage struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ModulePackage
	// +required
	Spec ModulePackageSpec `json:"spec"`

	// status defines the observed state of ModulePackage
	// +optional
	Status ModulePackageStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ModulePackageList contains a list of ModulePackage.
type ModulePackageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ModulePackage `json:"items"`
}

// GetConditions returns the status conditions of the ModulePackage.
func (in *ModulePackage) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the ModulePackage.
func (in *ModulePackage) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(GroupVersion, &ModulePackage{}, &ModulePackageList{})
		return nil
	})
}
