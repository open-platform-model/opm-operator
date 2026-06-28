package core_test

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	librarycore "github.com/open-platform-model/library/opm/core"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

func TestResourceFromCompiled_CopiesFields(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(deploymentCUE)
	require.NoError(t, v.Err())

	compiled := &librarycore.Compiled{
		Value:       v,
		Instance:    "test-instance",
		Component:   "web",
		Transformer: "transformers/deployment-transformer@v0.1.0",
	}

	got := core.ResourceFromCompiled(compiled)
	require.NotNil(t, got)

	assert.Equal(t, compiled.Instance, got.Instance)
	assert.Equal(t, compiled.Component, got.Component)
	assert.Equal(t, compiled.Transformer, got.Transformer)

	// The CUE value is copied through and remains usable via Resource accessors.
	assert.Equal(t, "Deployment", got.Kind())
	assert.Equal(t, "my-app", got.Name())
	assert.Equal(t, "default", got.Namespace())
}

func TestResourceFromCompiled_NilInput(t *testing.T) {
	assert.Nil(t, core.ResourceFromCompiled(nil))
}
