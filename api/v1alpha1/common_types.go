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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SourceReference points to a Flux source object.
// Used by BundleRelease; ModuleRelease uses CUE-native module resolution instead.
type SourceReference = fluxmeta.NamespacedObjectKindReference

// ModuleReference identifies the CUE module to evaluate from an OCI registry.
type ModuleReference struct {
	// Path is the CUE module import path.
	// Example: "opmodel.dev/modules/cert_manager@v0"
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`

	// Version is the pinned module version to resolve from the registry.
	// Example: "v0.2.1"
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

// RolloutSpec configures apply behavior for a release.
type RolloutSpec struct {
	// Strategy controls how apply operations are performed.
	// +kubebuilder:validation:Enum=Apply
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// ForceConflicts enables SSA force ownership when desired.
	// +optional
	ForceConflicts bool `json:"forceConflicts,omitempty"`
}

// SourceStatus describes the resolved source artifact used by a reconcile.
type SourceStatus struct {
	// Ref is the resolved source reference.
	// +optional
	Ref *SourceReference `json:"ref,omitempty"`

	// ArtifactRevision is the revision reported by the source artifact.
	// +optional
	ArtifactRevision string `json:"artifactRevision,omitempty"`

	// ArtifactDigest is the digest reported by the source artifact.
	// +optional
	ArtifactDigest string `json:"artifactDigest,omitempty"`

	// ArtifactURL is the fetch URL reported by the source artifact.
	// +optional
	ArtifactURL string `json:"artifactURL,omitempty"`
}

// FailureCounters tracks bounded reconcile failure counts by action.
type FailureCounters struct {
	// +optional
	Reconcile int64 `json:"reconcile,omitempty"`

	// +optional
	Apply int64 `json:"apply,omitempty"`

	// +optional
	Prune int64 `json:"prune,omitempty"`

	// +optional
	Drift int64 `json:"drift,omitempty"`
}

// InventoryEntry identifies one owned Kubernetes resource.
type InventoryEntry struct {
	// +optional
	Group string `json:"group,omitempty"`

	Kind string `json:"kind"`

	// +optional
	Namespace string `json:"namespace,omitempty"`

	Name string `json:"name"`

	// +optional
	Version string `json:"v,omitempty"`

	// +optional
	Component string `json:"component,omitempty"`
}

// Inventory stores the current set of owned resources.
type Inventory struct {
	// +optional
	Revision int64 `json:"revision,omitempty"`

	// +optional
	Digest string `json:"digest,omitempty"`

	// +optional
	Count int64 `json:"count,omitempty"`

	// +optional
	Entries []InventoryEntry `json:"entries,omitempty"`
}

// HistoryEntry captures a compact reconcile history record.
type HistoryEntry struct {
	// +optional
	Sequence int64 `json:"sequence,omitempty"`

	// +optional
	Action string `json:"action,omitempty"`

	// +optional
	Phase string `json:"phase,omitempty"`

	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// +optional
	SourceDigest string `json:"sourceDigest,omitempty"`

	// +optional
	ConfigDigest string `json:"configDigest,omitempty"`

	// +optional
	RenderDigest string `json:"renderDigest,omitempty"`

	// +optional
	InventoryDigest string `json:"inventoryDigest,omitempty"`

	// +optional
	InventoryCount int64 `json:"inventoryCount,omitempty"`

	// +optional
	Message string `json:"message,omitempty"`
}

// ModuleStatusSummary reports a child module status summary for bundles.
type ModuleStatusSummary struct {
	Name string `json:"name"`

	ReleaseRef fluxmeta.NamespacedObjectReference `json:"releaseRef"`

	// +optional
	Ready bool `json:"ready,omitempty"`

	// +optional
	SourceDigest string `json:"sourceDigest,omitempty"`

	// +optional
	ConfigDigest string `json:"configDigest,omitempty"`

	// +optional
	RenderDigest string `json:"renderDigest,omitempty"`

	// +optional
	InventoryCount int64 `json:"inventoryCount,omitempty"`
}

// RawValues stores arbitrary CUE/JSON-compatible values.
type RawValues struct {
	apiextensionsv1.JSON `json:",inline"`
}
