package resourceorder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetWeightKnownGVK(t *testing.T) {
	gvk := schema.GroupVersionKind{
		Group: "apps", Version: "v1", Kind: "Deployment",
	}
	assert.Equal(t, WeightDeployment, GetWeight(gvk))
}

func TestGetWeightCoreService(t *testing.T) {
	gvk := schema.GroupVersionKind{
		Group: "", Version: "v1", Kind: "Service",
	}
	assert.Equal(t, WeightService, GetWeight(gvk))
}

func TestGetWeightCRD(t *testing.T) {
	gvk := schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	}
	assert.Equal(t, WeightCRD, GetWeight(gvk))
}

func TestGetWeightUnknown(t *testing.T) {
	gvk := schema.GroupVersionKind{
		Group: "example.com", Version: "v1", Kind: "Foo",
	}
	assert.Equal(t, WeightDefault, GetWeight(gvk))
}

func TestGetWeightKindFallback(t *testing.T) {
	gvk := schema.GroupVersionKind{
		Group: "unknown.io", Version: "v99", Kind: "Deployment",
	}
	assert.Equal(t, WeightDeployment, GetWeight(gvk))
}
