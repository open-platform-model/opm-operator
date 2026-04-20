package inventory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

func makeResource(group, version, kind, namespace, name, component string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	obj.SetNamespace(namespace)
	obj.SetName(name)
	obj.SetLabels(map[string]string{core.LabelComponentName: component})
	return obj
}

func TestNewEntryFromResource_Namespaced(t *testing.T) {
	r := makeResource("apps", "v1", "Deployment", "production", "my-app", "app")
	entry := NewEntryFromResource(r)
	assert.Equal(t, "apps", entry.Group)
	assert.Equal(t, "Deployment", entry.Kind)
	assert.Equal(t, "production", entry.Namespace)
	assert.Equal(t, "my-app", entry.Name)
	assert.Equal(t, "v1", entry.Version)
	assert.Equal(t, "app", entry.Component)
}

func TestNewEntryFromResource_ClusterScoped(t *testing.T) {
	r := makeResource("", "v1", "Namespace", "", "default", "")
	entry := NewEntryFromResource(r)
	assert.Equal(t, "", entry.Group)
	assert.Equal(t, "Namespace", entry.Kind)
	assert.Equal(t, "", entry.Namespace)
	assert.Equal(t, "default", entry.Name)
	assert.Equal(t, "v1", entry.Version)
	assert.Equal(t, "", entry.Component)
}

func TestIdentityEqual_VersionExcluded(t *testing.T) {
	a := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"}
	b := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v2", Component: "web"}
	assert.True(t, IdentityEqual(a, b), "version should be excluded from identity comparison")
}

func TestIdentityEqual_ComponentIncluded(t *testing.T) {
	a := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"}
	c := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "frontend"}
	assert.False(t, IdentityEqual(a, c), "different components should not be identity-equal")
}

func TestK8sIdentityEqual_ComponentExcluded(t *testing.T) {
	a := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "web"}
	c := releasesv1alpha1.InventoryEntry{Group: "apps", Kind: "Deployment", Namespace: "ns", Name: "app", Version: "v1", Component: "frontend"}
	assert.True(t, K8sIdentityEqual(a, c), "K8sIdentityEqual should exclude component")
}
