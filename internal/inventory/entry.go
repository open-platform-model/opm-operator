package inventory

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// NewEntryFromResource creates an inventory entry from an unstructured Kubernetes resource.
// Extracts GVK, namespace, name, and the component label.
func NewEntryFromResource(r *unstructured.Unstructured) releasesv1alpha1.InventoryEntry {
	gvk := r.GroupVersionKind()
	labels := r.GetLabels()
	component := labels[core.LabelComponentName]
	return releasesv1alpha1.InventoryEntry{
		Group:     gvk.Group,
		Kind:      gvk.Kind,
		Namespace: r.GetNamespace(),
		Name:      r.GetName(),
		Version:   gvk.Version,
		Component: component,
	}
}

// IdentityEqual returns true if two entries identify the same owned resource,
// component-aware. Compares Group, Kind, Namespace, Name, and Component.
// Version is excluded to prevent false orphans during Kubernetes API version
// migrations.
//
// This helper is NOT the comparator used by ComputeStaleSet — that uses
// K8sIdentityEqual so component renames (same GVK+namespace+name, different
// component label) do not produce stale entries for live objects that SSA
// apply patches in place. Callers needing K8s-resource identity (the apiserver
// view: one live object per GVK+namespace+name) MUST use K8sIdentityEqual.
func IdentityEqual(a, b releasesv1alpha1.InventoryEntry) bool {
	return a.Group == b.Group &&
		a.Kind == b.Kind &&
		a.Namespace == b.Namespace &&
		a.Name == b.Name &&
		a.Component == b.Component
}

// K8sIdentityEqual returns true if two entries identify the same Kubernetes resource.
// Compares Group, Kind, Namespace, and Name only (excludes Version and Component).
func K8sIdentityEqual(a, b releasesv1alpha1.InventoryEntry) bool {
	return a.Group == b.Group &&
		a.Kind == b.Kind &&
		a.Namespace == b.Namespace &&
		a.Name == b.Name
}
