package render

import (
	"errors"
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oerrors "github.com/open-platform-model/library/opm/errors"
)

// An error with no CUE error in its chain keeps its own text.
func TestWithFindings_NonCUEErrorKeepsItsText(t *testing.T) {
	err := fmt.Errorf("Kernel.SynthesizeInstance: %w", errors.New("registry unreachable"))
	assert.Equal(t, "Kernel.SynthesizeInstance: registry unreachable", withFindings(err))
}

// A wrapped CUE error list prints its first finding and a count; withFindings
// keeps the frame before that finding and writes every finding out with its
// positions.
func TestWithFindings_WritesEveryFindingWithItsPositions(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString("note: string\nother: int\n", cue.Filename("schema.cue"))
	values := ctx.CompileString(`{"note":7,"other":"x"}`, cue.Filename("spec.values"))
	cueErr := schema.Unify(values).Validate()
	require.Error(t, cueErr)

	err := fmt.Errorf(`Kernel.SynthesizeInstance: instance "demo": %w`, cueErr)
	require.Contains(t, err.Error(), "(and 1 more errors)", "the plain text shortens the second finding to a count")

	assert.Equal(t,
		`Kernel.SynthesizeInstance: instance "demo": `+
			"note: conflicting values string and 7 (mismatched types string and int) (schema.cue:1:7, spec.values:1:1, spec.values:1:9); "+
			`other: conflicting values int and "x" (mismatched types int and string) (schema.cue:2:8, spec.values:1:1, spec.values:1:19)`,
		withFindings(err))
}

// The wording keeps the error chain: the reconcile classifiers read a
// registry fetch failure and the typed terminal causes through it.
func TestFindingsError_KeepsTheChain(t *testing.T) {
	fetch := &oerrors.FetchError{Kind: oerrors.FetchUnreachable, Err: errors.New("dial tcp: refused")}
	cause := fmt.Errorf("Kernel.SynthesizeInstance: %w", errors.Join(fetch, oerrors.ErrMissingRequiredField))
	err := error(&findingsError{msg: "synthesizing release: " + withFindings(cause), err: cause})

	assert.Equal(t, "synthesizing release: "+cause.Error(), err.Error())
	got, ok := errors.AsType[*oerrors.FetchError](err)
	require.True(t, ok)
	assert.Same(t, fetch, got)
	assert.ErrorIs(t, err, oerrors.ErrMissingRequiredField)
	assert.NotErrorIs(t, err, ErrAcquire)
}
