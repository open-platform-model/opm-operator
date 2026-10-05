package render

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oerrors "github.com/open-platform-model/library/opm/errors"
)

// valuesConflict returns the CUE error list of unifying spec.values with a
// #config that declares message as a string: one conflict, attributed to
// the values source at its own positions.
func valuesConflict(t *testing.T) error {
	t.Helper()
	ctx := cuecontext.New()
	schema := ctx.CompileString(`#config: message: string`)
	values := ctx.CompileString(`{"message": 42}`, cue.Filename(valuesOrigin))
	err := schema.LookupPath(cue.ParsePath("#config")).Unify(values).Validate()
	require.Error(t, err, "the test values must conflict with the schema")
	return err
}

// findingsList returns a CUE error list of n distinct findings.
func findingsList(n int) error {
	var list cueerrors.Error
	for i := range n {
		list = cueerrors.Append(list, cueerrors.Newf(token.NoPos, "finding %d", i))
	}
	return list
}

func TestSynthesisError_ListsEveryFindingWithItsPositions(t *testing.T) {
	cerr := valuesConflict(t)
	libErr := fmt.Errorf("Kernel.SynthesizeInstance: instance %q: %w", "x", cerr)

	msg := (&synthesisError{err: libErr}).Error()

	assert.True(t, strings.HasPrefix(msg, `synthesizing release: Kernel.SynthesizeInstance: instance "x": `), msg)
	assert.Contains(t, msg, "#config.message: conflicting values string and 42")
	assert.Contains(t, msg, "spec.values:1:13", "each finding carries its positions in the values source")
	assert.NotEqual(t, "synthesizing release: "+libErr.Error(), msg,
		"the findings replace CUE's one-line summary")
}

// A registry fetch failure keeps the library's message, even though its
// cause is a CUE error list (the loader classifies cue/load's error), and
// the typed failure is still found by type.
func TestSynthesisError_FetchFailureReadsUnchanged(t *testing.T) {
	cause := valuesConflict(t)
	fetch := &oerrors.FetchError{Kind: oerrors.FetchUnreachable, Err: cause}
	libErr := fmt.Errorf("Kernel.SynthesizeInstance: loading module package from /m (.): %w", fetch)

	err := error(&synthesisError{err: libErr})

	assert.Equal(t, "synthesizing release: "+libErr.Error(), err.Error())
	got, ok := errors.AsType[*oerrors.FetchError](err)
	require.True(t, ok, "the typed fetch failure is found through the wrapper")
	assert.Same(t, fetch, got)
	assert.True(t, errors.Is(err, oerrors.ErrTransient))
}

func TestSynthesisError_NoCUEErrorReadsUnchanged(t *testing.T) {
	libErr := fmt.Errorf("Kernel.SynthesizeInstance: %w", context.DeadlineExceeded)

	err := error(&synthesisError{err: libErr})

	assert.Equal(t, "synthesizing release: Kernel.SynthesizeInstance: context deadline exceeded", err.Error())
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// A frame the CUE error's message does not end the chain's message with
// (here the wrap appends text after it) falls back to the full message.
func TestSynthesisError_UnrecognisedFrameFallsBack(t *testing.T) {
	cerr := valuesConflict(t)
	libErr := fmt.Errorf("Kernel.SynthesizeInstance: %w (while synthesizing)", cerr)

	assert.Equal(t, "synthesizing release: "+libErr.Error(), (&synthesisError{err: libErr}).Error())
}

func TestCueFindings_CapsTheList(t *testing.T) {
	msg := cueFindings(findingsList(12))

	assert.Equal(t, 10, strings.Count(msg, "finding "), msg)
	assert.Contains(t, msg, "finding 9")
	assert.NotContains(t, msg, "finding 10")
	assert.True(t, strings.HasSuffix(msg, "; and 2 more"), msg)
}

func TestCueFindings_ListsUpToTheCap(t *testing.T) {
	msg := cueFindings(findingsList(10))

	assert.Equal(t, 10, strings.Count(msg, "finding "), msg)
	assert.NotContains(t, msg, "more")
}
