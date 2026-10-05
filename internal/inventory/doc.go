// Package inventory is the boundary between the API's inventory entry
// (v1alpha1.InventoryEntry, the shape status.inventory stores) and the
// library's opm/k8s/inventory, which owns the entry, the stale set and both
// digests. It holds the Current alias and the lossless conversions between
// the two entry types, and nothing else: an identity relation, a stale set
// or a digest belongs to the library (0012:D7).
package inventory
