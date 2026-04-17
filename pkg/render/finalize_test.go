package render

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalizeValue(t *testing.T) {
	ctx := cuecontext.New()

	v := ctx.CompileString(`{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: name: "my-app"
	}`)
	require.NoError(t, v.Err())

	out, err := FinalizeValue(ctx, v)
	require.NoError(t, err)
	assert.NoError(t, out.Err())

	name, err := out.LookupPath(cue.ParsePath("metadata.name")).String()
	require.NoError(t, err)
	assert.Equal(t, "my-app", name)
}
