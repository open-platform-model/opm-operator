package status

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/pkg/core"
)

func rawValues(jsonStr string) *releasesv1alpha1.RawValues {
	return &releasesv1alpha1.RawValues{
		JSON: apiextensionsv1.JSON{Raw: []byte(jsonStr)},
	}
}

func testResource(t *testing.T, cueSrc string) *core.Resource {
	t.Helper()
	ctx := cuecontext.New()
	v := ctx.CompileString(cueSrc)
	require.NoError(t, v.Err())
	return &core.Resource{Value: v}
}

// --- ConfigDigest tests ---

func TestConfigDigest_Deterministic(t *testing.T) {
	a := rawValues(`{"b":"2","a":"1"}`)
	b := rawValues(`{"a":"1","b":"2"}`)
	da := ConfigDigest(a)
	db := ConfigDigest(b)
	assert.Equal(t, da, db, "same logical JSON should produce same digest")
	assert.Contains(t, da, "sha256:")
}

func TestConfigDigest_NilValues(t *testing.T) {
	d := ConfigDigest(nil)
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", d,
		"nil values should hash empty input, consistent with inventory.ComputeDigest(nil)")
}

func TestConfigDigest_EmptyRaw(t *testing.T) {
	v := &releasesv1alpha1.RawValues{}
	d := ConfigDigest(v)
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", d,
		"empty raw should hash empty input, consistent with inventory.ComputeDigest(nil)")
}

func TestConfigDigest_ContentSensitive(t *testing.T) {
	a := rawValues(`{"key":"value-a"}`)
	b := rawValues(`{"key":"value-b"}`)
	assert.NotEqual(t, ConfigDigest(a), ConfigDigest(b),
		"different values should produce different digests")
}

// --- RenderDigest tests ---

func TestRenderDigest_OrderIndependent(t *testing.T) {
	deploy := testResource(t, `{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: { name: "app", namespace: "ns" }
		spec: replicas: 1
	}`)
	svc := testResource(t, `{
		apiVersion: "v1"
		kind:       "Service"
		metadata: { name: "svc", namespace: "ns" }
		spec: type: "ClusterIP"
	}`)

	d1, err := RenderDigest([]*core.Resource{deploy, svc})
	require.NoError(t, err)
	d2, err := RenderDigest([]*core.Resource{svc, deploy})
	require.NoError(t, err)
	assert.Equal(t, d1, d2, "order should not affect digest")
	assert.Contains(t, d1, "sha256:")
}

func TestRenderDigest_ContentSensitive(t *testing.T) {
	a := testResource(t, `{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: { name: "app-a", namespace: "ns" }
	}`)
	b := testResource(t, `{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: { name: "app-b", namespace: "ns" }
	}`)

	da, err := RenderDigest([]*core.Resource{a})
	require.NoError(t, err)
	db, err := RenderDigest([]*core.Resource{b})
	require.NoError(t, err)
	assert.NotEqual(t, da, db, "different resources should produce different digests")
}

func TestRenderDigest_Empty(t *testing.T) {
	d, err := RenderDigest(nil)
	require.NoError(t, err)
	assert.Contains(t, d, "sha256:")
}

// --- IsNoOp tests ---

func TestIsNoOp_AllMatch(t *testing.T) {
	ds := DigestSet{
		Source:    "sha256:aaa",
		Config:    "sha256:bbb",
		Render:    "sha256:ccc",
		Inventory: "sha256:ddd",
	}
	assert.True(t, IsNoOp(ds, ds))
}

func TestIsNoOp_OneDiffers(t *testing.T) {
	current := DigestSet{
		Source:    "sha256:aaa",
		Config:    "sha256:bbb",
		Render:    "sha256:ccc",
		Inventory: "sha256:ddd",
	}
	changed := "sha256:xxx"
	fields := []string{"Source", "Config", "Render", "Inventory"}
	for _, field := range fields {
		last := current
		switch field {
		case "Source":
			last.Source = changed
		case "Config":
			last.Config = changed
		case "Render":
			last.Render = changed
		case "Inventory":
			last.Inventory = changed
		}
		assert.False(t, IsNoOp(current, last), "should not be no-op when %s differs", field)
	}
}

func TestIsNoOp_EmptyLastApplied(t *testing.T) {
	current := DigestSet{
		Source:    "sha256:aaa",
		Config:    "sha256:bbb",
		Render:    "sha256:ccc",
		Inventory: "sha256:ddd",
	}
	assert.False(t, IsNoOp(current, DigestSet{}), "empty last applied = first reconcile, not a no-op")
}
