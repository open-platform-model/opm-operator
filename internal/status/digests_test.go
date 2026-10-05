package status

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

func rawValues(jsonStr string) *releasesv1alpha1.RawValues {
	return &releasesv1alpha1.RawValues{
		JSON: apiextensionsv1.JSON{Raw: []byte(jsonStr)},
	}
}

func testResource(t *testing.T, cueSrc string) *object.Resource {
	t.Helper()
	ctx := cuecontext.New()
	v := ctx.CompileString(cueSrc)
	require.NoError(t, v.Err())
	return &object.Resource{Value: v}
}

// exported runs the library's single export over resources, as the
// reconciler does before it computes the render digest.
func exported(t *testing.T, resources ...*object.Resource) []object.Exported {
	t.Helper()
	out, err := object.Export(resources)
	require.NoError(t, err)
	return out
}

// --- ModuleSourceDigest tests ---

// TestModuleSourceDigest_GoldenPin freezes the source-digest formula as a
// cross-repo contract: exactly sha256(modulePath + "@" + moduleVersion)
// rendered as "sha256:%x". The CLI's sourceDigest (cli
// internal/workflow/apply) is the byte-identical peer — the two actors'
// no-op detection depends on the equality, so neither side may change the
// formula unilaterally. If this pin fails, coordinate with the CLI before
// touching the formula.
func TestModuleSourceDigest_GoldenPin(t *testing.T) {
	d := ModuleSourceDigest("opmodel.dev/modules/test/podinfo@v0", "v0.1.3")
	assert.Equal(t,
		"sha256:08e4e4ee0f89463469644b41018405fabe4289b3deef1b8b6fd321cf6569aed5", d)
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

	d1 := RenderDigest(exported(t, deploy, svc))
	d2 := RenderDigest(exported(t, svc, deploy))
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

	da := RenderDigest(exported(t, a))
	db := RenderDigest(exported(t, b))
	assert.NotEqual(t, da, db, "different resources should produce different digests")
}

// TestRenderDigest_GoldenBytes pins the render digest of a fixed set, given
// out of sort order. lastAppliedRenderDigest gates no-op detection, so a
// change to these bytes makes every applied instance re-apply once after an
// upgrade. The literal was recorded before the digest read the library's
// export; never edit it to make a refactor pass.
func TestRenderDigest_GoldenBytes(t *testing.T) {
	role := testResource(t, `{
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "ClusterRole"
		metadata: name: "reader"
		rules: [{apiGroups: [""], resources: ["pods"], verbs: ["get", "list"]}]
	}`)
	svc := testResource(t, `{
		apiVersion: "v1"
		kind:       "Service"
		metadata: { name: "web", namespace: "apps" }
		spec: { type: "ClusterIP", ports: [{port: 80}] }
	}`)
	deploy := testResource(t, `{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: { name: "web", namespace: "apps", labels: app: "web" }
		spec: replicas: 2
	}`)

	d := RenderDigest(exported(t, svc, deploy, role))
	assert.Equal(t, "sha256:27ca646c68345860d39c6bf8409e9a8dc4df1980a24b483a60af1b759545dfea", d)
}

func TestRenderDigest_Empty(t *testing.T) {
	d := RenderDigest(nil)
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

// --- RenderInputKey tests ---

func fullKey() RenderInputKey {
	return RenderInputKey{
		Source:          "sha256:src",
		Config:          "sha256:cfg",
		PackageIdentity: "gen-3",
		SkewPolicy:      "Warn",
		OperatorVersion: "v1.0.0-beta.9",
		LibraryVersion:  "v1.0.0-beta.4",
	}
}

func TestRenderInputKey_EachPartChangesTheDigest(t *testing.T) {
	base := fullKey().Digest()
	mutations := map[string]func(*RenderInputKey){
		"source":   func(k *RenderInputKey) { k.Source = "sha256:other" },
		"config":   func(k *RenderInputKey) { k.Config = "sha256:other" },
		"identity": func(k *RenderInputKey) { k.PackageIdentity = "gen-4" },
		"skew":     func(k *RenderInputKey) { k.SkewPolicy = "Refuse" },
		"operator": func(k *RenderInputKey) { k.OperatorVersion = "v1.0.0-beta.10" },
		"library":  func(k *RenderInputKey) { k.LibraryVersion = "v1.0.0-beta.5" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			k := fullKey()
			mutate(&k)
			assert.NotEqual(t, base, k.Digest())
		})
	}
}

func TestRenderInputKey_SamePartsSameDigest(t *testing.T) {
	assert.Equal(t, fullKey().Digest(), fullKey().Digest())
}

func TestRenderInputKey_PartBoundariesAreUnambiguous(t *testing.T) {
	a, b := fullKey(), fullKey()
	a.SkewPolicy, a.OperatorVersion = "ab", "c"
	b.SkewPolicy, b.OperatorVersion = "a", "bc"
	assert.NotEqual(t, a.Digest(), b.Digest())
}

func TestRenderInputKey_CompleteNeedsEveryPart(t *testing.T) {
	assert.True(t, fullKey().Complete())
	clears := map[string]func(*RenderInputKey){
		"source":   func(k *RenderInputKey) { k.Source = "" },
		"config":   func(k *RenderInputKey) { k.Config = "" },
		"identity": func(k *RenderInputKey) { k.PackageIdentity = "" },
		"skew":     func(k *RenderInputKey) { k.SkewPolicy = "" },
		"operator": func(k *RenderInputKey) { k.OperatorVersion = "" },
		"library":  func(k *RenderInputKey) { k.LibraryVersion = "" },
	}
	for name, clear := range clears {
		t.Run(name, func(t *testing.T) {
			k := fullKey()
			clear(&k)
			assert.False(t, k.Complete())
		})
	}
}

// TestRenderInputKey_GoldenDigest pins the encoding: a change here changes
// every recorded key, which is allowed only together with a new encoding tag.
func TestRenderInputKey_GoldenDigest(t *testing.T) {
	assert.Equal(t, "sha256:548bd49f63a4c8cb9ee0a71469dec1e100fa8d1b2eb06fb28171b7aa72424cb9", fullKey().Digest())
}
