package render

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-platform-model/library/opm/module"
)

func TestDeclaredModuleVersion(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "concrete version", src: `#module: metadata: version: "0.1.0"`, want: "0.1.0"},
		{name: "no source module", src: `metadata: name: "web"`, want: ""},
		{name: "no version", src: `#module: metadata: name: "web"`, want: ""},
		{name: "version not concrete", src: `#module: metadata: version: string`, want: ""},
		{name: "version not a string", src: `#module: metadata: version: 1`, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := cuecontext.New().CompileString(tc.src)
			require.NoError(t, v.Err())
			assert.Equal(t, tc.want, declaredModuleVersion(&module.Instance{Package: v}))
		})
	}

	t.Run("nil instance", func(t *testing.T) {
		assert.Equal(t, "", declaredModuleVersion(nil))
	})
}
