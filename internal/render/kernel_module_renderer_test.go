package render

import (
	"errors"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-platform-model/library/opm/helper/objectset"
	"github.com/open-platform-model/library/opm/kernel"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
)

// compiledObject builds one rendered object the way the kernel hands it to
// the adapter: the object's CUE value plus the component and transformer
// that produced it.
func compiledObject(t *testing.T, ctx *cue.Context, component, transformer, src string) *kernel.Compiled {
	t.Helper()
	v := ctx.CompileString(src)
	require.NoError(t, v.Err(), "compiling the test object")
	return &kernel.Compiled{
		Value:       v,
		Instance:    "web",
		Component:   component,
		Transformer: transformer,
	}
}

const registrationTransformer = "opmodel.dev/catalogs/opm/transformers/transformer-registration@v4"

// The motivating case of 0015:D15: a module shipping two
// transformer-registration components renders two cluster-scoped objects
// under one instance-derived name. The adapter refuses before anything is
// built, with the library's typed error naming the identity once and both
// carrying components with their transformers.
func TestResultFromRender_RefusesTwoRegistrationsUnderOneName(t *testing.T) {
	ctx := cuecontext.New()
	out := &kernel.RenderResult{Compiled: []*kernel.Compiled{
		compiledObject(t, ctx, "registration", registrationTransformer, `{
			apiVersion: "opmodel.dev/v1alpha1"
			kind:       "TransformerRegistration"
			metadata: name: "web"
		}`),
		compiledObject(t, ctx, "registration-copy", registrationTransformer, `{
			apiVersion: "opmodel.dev/v1alpha1"
			kind:       "TransformerRegistration"
			metadata: name: "web"
		}`),
	}}

	res, err := resultFromRender(out, platformstore.PackageIdentity{}, nil)

	assert.Nil(t, res, "a refused render must produce no partial result")
	dupErr, ok := errors.AsType[*objectset.DuplicateIdentitiesError](err)
	require.True(t, ok, "the library error is returned bare, got: %v", err)
	require.Len(t, dupErr.Duplicates, 1)

	msg := err.Error()
	assert.Equal(t, 1, strings.Count(msg, "opmodel.dev/v1alpha1 TransformerRegistration web"),
		"the shared identity is named once, in: %s", msg)
	assert.Contains(t, msg, `component "registration" (`+registrationTransformer+`)`)
	assert.Contains(t, msg, `component "registration-copy" (`+registrationTransformer+`)`)
}

// The healthy path is untouched: objects with distinct identities adapt to
// one resource and one inventory entry each.
func TestResultFromRender_AdaptsDistinctIdentities(t *testing.T) {
	ctx := cuecontext.New()
	out := &kernel.RenderResult{Compiled: []*kernel.Compiled{
		compiledObject(t, ctx, "registration", registrationTransformer, `{
			apiVersion: "opmodel.dev/v1alpha1"
			kind:       "TransformerRegistration"
			metadata: name: "web"
		}`),
		compiledObject(t, ctx, "registration-other", registrationTransformer, `{
			apiVersion: "opmodel.dev/v1alpha1"
			kind:       "TransformerRegistration"
			metadata: name: "web-other"
		}`),
	}}

	res, err := resultFromRender(out, platformstore.PackageIdentity{}, nil)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Len(t, res.Resources, 2)
	assert.Len(t, res.InventoryEntries, 2)
}

// Only the shared identity is reported: a render carrying one duplicated
// Deployment and one distinct Service names the Deployment and nothing else,
// so the module author is pointed at the two components to fix.
func TestResultFromRender_NamesOnlyTheSharedIdentity(t *testing.T) {
	ctx := cuecontext.New()
	out := &kernel.RenderResult{Compiled: []*kernel.Compiled{
		compiledObject(t, ctx, "web", "opmodel.dev/catalogs/opm/transformers/deployment@v4", `{
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {name: "web", namespace: "web-system"}
		}`),
		compiledObject(t, ctx, "web-copy", "opmodel.dev/catalogs/opm/transformers/deployment@v4", `{
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {name: "web", namespace: "web-system"}
		}`),
		compiledObject(t, ctx, "web", "opmodel.dev/catalogs/opm/transformers/expose@v4", `{
			apiVersion: "v1"
			kind:       "Service"
			metadata: {name: "web", namespace: "web-system"}
		}`),
	}}

	res, err := resultFromRender(out, platformstore.PackageIdentity{}, nil)

	assert.Nil(t, res)
	dupErr, ok := errors.AsType[*objectset.DuplicateIdentitiesError](err)
	require.True(t, ok, "the library error is returned bare, got: %v", err)
	require.Len(t, dupErr.Duplicates, 1)
	assert.Equal(t, "Deployment", dupErr.Duplicates[0].Identity.Kind)

	msg := err.Error()
	assert.Contains(t, msg, "apps/v1 Deployment web-system/web")
	assert.NotContains(t, msg, "Service")
}
