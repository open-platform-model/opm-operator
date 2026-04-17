package inventory

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/pkg/core"
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

// IdentityEqual returns true if two entries identify the same owned resource.
// Compares Group, Kind, Namespace, Name, and Component. Version is excluded
// to prevent false orphans during Kubernetes API version migrations.
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
