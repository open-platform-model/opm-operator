package render

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"

	oerrors "github.com/open-platform-model/library/opm/errors"
)

const (
	// eventNoteLimit is the longest note events.k8s.io/v1 accepts, in bytes.
	eventNoteLimit = 1024
	// conditionMessageLimit is the longest message a metav1.Condition holds
	// (maxLength of the conditions' message in the generated CRDs).
	conditionMessageLimit = 32768
)

// bounded is an error that can word itself within a length. Its Error text
// is the wording within conditionMessageLimit.
type bounded interface {
	error
	within(limit int) string
}

// EventNote returns the text of a render failure for an event note: err's
// own text when it fits the note limit of events.k8s.io/v1. A longer failure
// that lists findings names as many whole findings as fit and counts the
// rest; any other long text is cut, with a count of the characters left out.
// The API server refuses an event with a longer note, so an uncut note loses
// the event.
func EventNote(err error) string {
	text := err.Error()
	if len(text) <= eventNoteLimit {
		return text
	}
	// Only a wording that is the whole text may replace it: a bounded error
	// under a wrap would lose what the wrap wrote in front.
	if b, ok := errors.AsType[bounded](err); ok && b.Error() == text {
		return b.within(eventNoteLimit)
	}
	return cutText(text, eventNoteLimit)
}

// findingsError words err with its CUE findings written out one by one, each
// with its positions, and keeps err's chain, so the classifiers still find a
// typed cause (a registry fetch failure, a terminal cause) under it.
//
// The text of an error that wraps a CUE error list holds the first finding, a
// count of the rest and no position. The wording is the frame (the caller's
// prefix and the kernel's own text in front of that first finding) followed
// by every finding of the typed error tree. An error with no finding keeps
// its own text as the frame.
type findingsError struct {
	frame    string
	findings []string
	err      error
}

func (e *findingsError) Error() string { return e.within(conditionMessageLimit) }
func (e *findingsError) Unwrap() error { return e.err }

// within words the error in at most limit bytes: every finding when they
// fit, else as many whole findings as fit and a count of the rest.
func (e *findingsError) within(limit int) string {
	if list, ok := listWithin(e.findings, limit-len(e.frame)); ok {
		return e.frame + list
	}
	return cutText(e.frame+strings.Join(e.findings, "; "), limit)
}

// withFindings words err as prefix, the text err holds in front of its first
// CUE finding, and every finding with its positions. A position in a file
// under root is written relative to root; root "" leaves every position as
// CUE reports it. An error with no CUE error in its chain, one whose text does
// not hold its first finding, and a registry fetch failure read prefix + their
// own text.
func withFindings(prefix string, err error, root string) *findingsError {
	text := err.Error()
	worded := &findingsError{frame: prefix + text, err: err}

	// A registry fetch failure is not a list of findings to act on: its text
	// ends in the registry's answer and stays as it is.
	if _, isFetch := errors.AsType[*oerrors.FetchError](err); isFetch {
		return worded
	}
	var ce cueerrors.Error
	if !errors.As(err, &ce) {
		return worded
	}
	list := cueerrors.Errors(err)
	if len(list) == 0 {
		return worded
	}
	i := strings.Index(text, list[0].Error())
	if i < 0 {
		return worded
	}
	worded.frame = prefix + text[:i]
	worded.findings = make([]string, 0, len(list))
	for _, finding := range list {
		worded.findings = append(worded.findings, findingText(finding, root))
	}
	return worded
}

// findingText words one CUE finding followed by the positions CUE attributed
// it to, so a values violation reads
// `message: conflicting values ... (spec.values:1:13, ...)`.
func findingText(finding cueerrors.Error, root string) string {
	line := finding.Error()
	positions := cueerrors.Positions(finding)
	if len(positions) == 0 {
		return line
	}
	at := make([]string, 0, len(positions))
	for _, p := range positions {
		at = append(at, positionText(p, root))
	}
	return line + " (" + strings.Join(at, ", ") + ")"
}

// positionText is p as file:line:column, with the file relative to root when
// it lies under root.
func positionText(p token.Pos, root string) string {
	text := p.String()
	if root == "" {
		return text
	}
	if rel, ok := strings.CutPrefix(text, root+string(filepath.Separator)); ok {
		return rel
	}
	return text
}

// cueModuleRoot returns the nearest directory at or above dir that holds a
// cue.mod directory, and dir itself when there is none.
func cueModuleRoot(dir string) string {
	dir = filepath.Clean(dir)
	for d := dir; ; d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, "cue.mod")); err == nil && info.IsDir() {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

// listWithin joins items with "; " in at most budget bytes. When they do not
// all fit it keeps as many whole items as fit, from the first, and ends with
// a count of the rest. It reports false when not even the first item fits
// beside that count.
func listWithin(items []string, budget int) (string, bool) {
	full := strings.Join(items, "; ")
	if len(full) <= budget {
		return full, true
	}
	// Room for the items: the budget less the longest possible tail.
	room := budget - len(moreFindings(len(items)))
	used, kept := 0, 0
	for _, item := range items {
		cost := len(item)
		if kept > 0 {
			cost += len("; ")
		}
		if used+cost > room {
			break
		}
		used += cost
		kept++
	}
	if kept == 0 {
		return "", false
	}
	return strings.Join(items[:kept], "; ") + moreFindings(len(items)-kept), true
}

// moreFindings is the tail of a list that leaves n findings out.
func moreFindings(n int) string { return fmt.Sprintf("; and %d more findings", n) }

// cutText returns text in at most limit bytes: text itself when it fits,
// else its start, cut between two characters, and a count of the characters
// left out.
func cutText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	const tail = " ... (%d more characters)"
	keep := max(limit-len(fmt.Sprintf(tail, len(text))), 0)
	for keep > 0 && !utf8.RuneStart(text[keep]) {
		keep--
	}
	return text[:keep] + fmt.Sprintf(tail, utf8.RuneCountInString(text[keep:]))
}
