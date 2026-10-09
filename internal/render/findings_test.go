package render

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oerrors "github.com/open-platform-model/library/opm/errors"
)

// An error with no CUE error in its chain keeps its own text.
func TestWithFindings_NonCUEErrorKeepsItsText(t *testing.T) {
	err := fmt.Errorf("Kernel.SynthesizeInstance: %w", errors.New("registry unreachable"))
	assert.Equal(t, "synthesizing release: Kernel.SynthesizeInstance: registry unreachable",
		withFindings("synthesizing release: ", err, "").Error())
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
		withFindings("", err, "").Error())
}

// A position in a file under the root is written relative to it; a position
// anywhere else is left as CUE reports it.
func TestWithFindings_PositionsUnderTheRootAreRelative(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString("note: string\n", cue.Filename("/cache/mod/schema.cue"))
	values := ctx.CompileString("note: 7\n", cue.Filename("/tmp/extract-1/pkg/instance.cue"))
	cueErr := schema.Unify(values).Validate()
	require.Error(t, cueErr)

	assert.Equal(t,
		"loading: note: conflicting values string and 7 (mismatched types string and int) "+
			"(/cache/mod/schema.cue:1:7, pkg/instance.cue:1:7)",
		withFindings("loading: ", cueErr, "/tmp/extract-1").Error())
	// A directory whose name only starts like the root is not under it.
	assert.Contains(t, withFindings("", cueErr, "/tmp/extract").Error(), "/tmp/extract-1/pkg/instance.cue:1:7")
}

// opaqueError hides its cause's text behind its own.
type opaqueError struct{ err error }

func (e *opaqueError) Error() string { return "opaque failure" }
func (e *opaqueError) Unwrap() error { return e.err }

// An error whose text does not hold its first CUE finding keeps its own text.
func TestWithFindings_TextWithoutTheFindingKeepsItsText(t *testing.T) {
	ctx := cuecontext.New()
	cueErr := ctx.CompileString("a: 1\na: 2\n", cue.Filename("x.cue")).Validate()
	require.Error(t, cueErr)

	assert.Equal(t, "opaque failure", withFindings("", &opaqueError{err: cueErr}, "").Error())
}

// The wording keeps the error chain: the reconcile classifiers read a
// registry fetch failure and the typed terminal causes through it.
func TestFindingsError_KeepsTheChain(t *testing.T) {
	fetch := &oerrors.FetchError{Kind: oerrors.FetchUnreachable, Err: errors.New("dial tcp: refused")}
	cause := fmt.Errorf("Kernel.SynthesizeInstance: %w", errors.Join(fetch, oerrors.ErrMissingRequiredField))
	err := error(withFindings("synthesizing release: ", cause, ""))

	assert.Equal(t, "synthesizing release: "+cause.Error(), err.Error())
	got, ok := errors.AsType[*oerrors.FetchError](err)
	require.True(t, ok)
	assert.Same(t, fetch, got)
	assert.ErrorIs(t, err, oerrors.ErrMissingRequiredField)
	assert.NotErrorIs(t, err, ErrAcquire)

	// An acquisition failure keeps the chain through the wording too.
	acquire := acquireFailed("loading package", withFindings("", cause, ""))
	assert.Equal(t, "loading package: "+cause.Error(), acquire.Error())
	assert.ErrorIs(t, acquire, ErrAcquire)
	_, ok = errors.AsType[*oerrors.FetchError](acquire)
	assert.True(t, ok)
}

// unsetValues returns a kernel-shaped refusal of n unset required values,
// v01 to vN, each one finding with one position.
func unsetValues(t *testing.T, n int) error {
	t.Helper()
	var schema strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&schema, "v%02d: string\n", i)
	}
	v := cuecontext.New().CompileString(schema.String(), cue.Filename("module.cue"))
	cueErr := v.Validate(cue.Concrete(true))
	require.Error(t, cueErr)
	return fmt.Errorf(`Kernel.SynthesizeInstance: instance "demo": not fully concrete: %w`, cueErr)
}

// countedTail reads the count from a text that ends "; and N more findings".
func countedTail(t *testing.T, text string) int {
	t.Helper()
	i := strings.LastIndex(text, "; and ")
	require.GreaterOrEqual(t, i, 0, "no count in: %s", text)
	var n int
	_, err := fmt.Sscanf(text[i:], "; and %d more findings", &n)
	require.NoError(t, err, "tail of: %s", text)
	return n
}

// Twenty unset required values: the error text, which becomes the condition
// message, names every one; the event note names as many whole findings as
// the note limit of events.k8s.io/v1 holds and counts the rest.
func TestEventNote_TwentyUnsetValuesAreCutToWholeFindingsAndACount(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(*findingsError) error
		head string
	}{
		{
			name: "ModuleInstance",
			wrap: func(e *findingsError) error { return e },
			head: `synthesizing release: Kernel.SynthesizeInstance: instance "demo": not fully concrete: `,
		},
		{
			name: "ModulePackage",
			wrap: func(e *findingsError) error { return acquireFailed("loading package", e) },
			head: `loading package: synthesizing release: Kernel.SynthesizeInstance: instance "demo": not fully concrete: `,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Positions in a long directory, as in the CUE module cache.
			worded := withFindings("synthesizing release: ", unsetValues(t, 20), "")
			long := strings.Repeat("d", 60) + "/module.cue"
			for i, f := range worded.findings {
				worded.findings[i] = strings.Replace(f, "module.cue", long, 1)
			}
			err := tc.wrap(worded)

			message := err.Error()
			assert.Less(t, len(message), conditionMessageLimit)
			assert.NotContains(t, message, " more ")
			for i := 1; i <= 20; i++ {
				assert.Contains(t, message, fmt.Sprintf("v%02d: incomplete value string (%s:%d:6)", i, long, i))
			}
			require.Greater(t, len(message), eventNoteLimit, "the case must need the cut")

			note := EventNote(err)
			assert.LessOrEqual(t, len(note), eventNoteLimit)
			left := countedTail(t, note)
			named := strings.Count(note, ": incomplete value string (")
			assert.Equal(t, 20, named+left, "every value is named or counted: %s", note)
			assert.Greater(t, named, 5)
			// The note is the message up to a whole finding, then the count.
			kept := strings.TrimSuffix(note, moreFindings(left))
			assert.True(t, strings.HasPrefix(message, kept+"; "), "the cut is between two findings: %s", note)
			assert.True(t, strings.HasPrefix(note, tc.head+"v01: "))
			assert.True(t, strings.HasSuffix(kept, ":"+fmt.Sprint(named)+":6)"), "the last finding kept is whole: %s", kept)
		})
	}
}

// A failure that fits the note limit is its own text; a long text with no
// findings, and a long wording under another wrap, are cut between two
// characters with a count of the characters left out.
func TestEventNote_OtherTexts(t *testing.T) {
	short := withFindings("synthesizing release: ", unsetValues(t, 3), "")
	assert.Equal(t, short.Error(), EventNote(short))
	assert.Equal(t, 2, strings.Count(short.Error(), "; "))

	plain := errors.New(strings.Repeat("é", 2000)) // 4000 bytes
	note := EventNote(plain)
	assert.LessOrEqual(t, len(note), eventNoteLimit)
	assert.True(t, utf8.ValidString(note))
	kept := strings.Count(note, "é")
	assert.True(t, strings.HasSuffix(note, fmt.Sprintf(" ... (%d more characters)", 2000-kept)), note)
	assert.Greater(t, kept, 450)

	wrapped := fmt.Errorf("rendering: %w", withFindings("", unsetValues(t, 40), ""))
	note = EventNote(wrapped)
	assert.LessOrEqual(t, len(note), eventNoteLimit)
	assert.True(t, strings.HasPrefix(note, "rendering: Kernel.SynthesizeInstance: "), "the wrap's own text is kept: %s", note)
	assert.Contains(t, note, " more characters)")

	// One finding longer than the limit is cut too.
	huge := &findingsError{frame: "frame: ", findings: []string{strings.Repeat("x", 3000), "second"}, err: errors.New("x")}
	note = EventNote(huge)
	assert.LessOrEqual(t, len(note), eventNoteLimit)
	assert.True(t, strings.HasPrefix(note, "frame: xxx"))
	assert.Contains(t, note, " more characters)")
}

// The condition message is bounded the same way at the condition's limit.
func TestFindingsError_ErrorStaysInsideTheConditionLimit(t *testing.T) {
	worded := withFindings("synthesizing release: ", unsetValues(t, 3), "")
	finding := worded.findings[0]
	worded.findings = nil
	for range 1000 {
		worded.findings = append(worded.findings, finding+strings.Repeat("p", 20))
	}
	message := worded.Error()
	assert.LessOrEqual(t, len(message), conditionMessageLimit)
	left := countedTail(t, message)
	assert.Equal(t, 1000, strings.Count(message, ": incomplete value string (")+left)
}
