package inventory

import (
	"testing"

	"github.com/stretchr/testify/assert"

	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

var roundTripEntries = []releasesv1alpha1.InventoryEntry{
	{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"},
	// A core-group, cluster-scoped entry: every optional field empty.
	{Kind: "Namespace", Name: "team"},
}

func TestConvert_RoundTripFromAPIEntry(t *testing.T) {
	for _, e := range roundTripEntries {
		assert.Equal(t, e, FromEntry(ToEntry(e)))
	}
	assert.Equal(t, roundTripEntries, FromEntries(ToEntries(roundTripEntries)))
}

func TestConvert_RoundTripFromLibraryEntry(t *testing.T) {
	lib := []k8sinventory.Entry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"},
		{Kind: "Namespace", Name: "team"},
	}
	for _, e := range lib {
		assert.Equal(t, e, ToEntry(FromEntry(e)))
	}
	assert.Equal(t, lib, ToEntries(FromEntries(lib)))
}

func TestConvert_FieldsMapOneToOne(t *testing.T) {
	got := ToEntry(roundTripEntries[0])
	assert.Equal(t, k8sinventory.Entry{
		Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web",
	}, got)

	empty := ToEntry(roundTripEntries[1])
	assert.Empty(t, empty.Group)
	assert.Empty(t, empty.Namespace)
	assert.Empty(t, empty.Version)
	assert.Empty(t, empty.Component)
}

func TestConvert_SlicesKeepNilAndEmpty(t *testing.T) {
	assert.Nil(t, ToEntries(nil))
	assert.Nil(t, FromEntries(nil))
	assert.NotNil(t, ToEntries([]releasesv1alpha1.InventoryEntry{}))
	assert.Empty(t, ToEntries([]releasesv1alpha1.InventoryEntry{}))
	assert.NotNil(t, FromEntries([]k8sinventory.Entry{}))
	assert.Empty(t, FromEntries([]k8sinventory.Entry{}))
}
