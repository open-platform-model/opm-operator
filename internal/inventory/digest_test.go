package inventory

import (
	"testing"

	"github.com/stretchr/testify/assert"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

func TestComputeDigest_Deterministic(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"},
		{Group: "", Kind: "Service", Namespace: "ns", Name: "svc", Version: "v1", Component: "web"},
	}
	reversed := []releasesv1alpha1.InventoryEntry{entries[1], entries[0]}
	assert.Equal(t, ComputeDigest(entries), ComputeDigest(reversed),
		"digest should be deterministic regardless of input order")
}

func TestComputeDigest_ContentSensitive(t *testing.T) {
	a := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app-a"},
	}
	b := []releasesv1alpha1.InventoryEntry{
		{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app-b"},
	}
	assert.NotEqual(t, ComputeDigest(a), ComputeDigest(b),
		"different content should produce different digests")
}

func TestComputeDigest_EmptyInput(t *testing.T) {
	digest := ComputeDigest(nil)
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", digest)
}

func TestComputeDigest_EmptySlice(t *testing.T) {
	digest := ComputeDigest([]releasesv1alpha1.InventoryEntry{})
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", digest)
}
