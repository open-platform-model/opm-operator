package inventory

import (
	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// ToEntry converts the API's inventory entry to the library's Entry. The six
// fields map one to one, so FromEntry(ToEntry(e)) == e.
func ToEntry(e releasesv1alpha1.InventoryEntry) k8sinventory.Entry {
	return k8sinventory.Entry{
		Group:     e.Group,
		Kind:      e.Kind,
		Namespace: e.Namespace,
		Name:      e.Name,
		Version:   e.Version,
		Component: e.Component,
	}
}

// FromEntry converts the library's Entry to the API's inventory entry, the
// shape status.inventory stores. ToEntry(FromEntry(e)) == e.
func FromEntry(e k8sinventory.Entry) releasesv1alpha1.InventoryEntry {
	return releasesv1alpha1.InventoryEntry{
		Group:     e.Group,
		Kind:      e.Kind,
		Namespace: e.Namespace,
		Name:      e.Name,
		Version:   e.Version,
		Component: e.Component,
	}
}

// ToEntries converts each entry with ToEntry, keeping order. A nil slice
// stays nil and an empty slice stays empty.
func ToEntries(entries []releasesv1alpha1.InventoryEntry) []k8sinventory.Entry {
	if entries == nil {
		return nil
	}
	out := make([]k8sinventory.Entry, len(entries))
	for i, e := range entries {
		out[i] = ToEntry(e)
	}
	return out
}

// FromEntries converts each entry with FromEntry, keeping order. A nil slice
// stays nil and an empty slice stays empty.
func FromEntries(entries []k8sinventory.Entry) []releasesv1alpha1.InventoryEntry {
	if entries == nil {
		return nil
	}
	out := make([]releasesv1alpha1.InventoryEntry, len(entries))
	for i, e := range entries {
		out[i] = FromEntry(e)
	}
	return out
}
