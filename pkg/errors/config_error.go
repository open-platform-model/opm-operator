package errors

import (
	"fmt"
	"path/filepath"
	"strings"

	cueerrors "cuelang.org/go/cue/errors"
)

// ConfigError is a structured validation error produced by the Module Gate
// when supplied values do not satisfy a #config schema.
//
// It carries the raw CUE error tree so callers can obtain a human-readable
// summary via Error() or grouped diagnostics via GroupedErrors().
type ConfigError struct {
	// Context identifies which gate produced the error (e.g. "module").
	Context string

	// Name is the release name for display (e.g. "my-game-stack", "server").
	Name string

	// RawError is the original CUE unification or concreteness error.
	RawError error
}

// Error implements the error interface.
// Produces a human-readable summary: one line per unique CUE error position.
func (e *ConfigError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %q: values do not satisfy #config:\n", e.Context, e.Name)

	for _, ce := range cueerrors.Errors(e.RawError) {
		pos := ce.Position()
		msg := cueerrors.Details(ce, nil)
		if pos.IsValid() {
			fmt.Fprintf(&sb, "  - %s: %s\n", pos, strings.TrimSpace(msg))
		} else {
			fmt.Fprintf(&sb, "  - %s\n", strings.TrimSpace(msg))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Unwrap returns the underlying CUE error for errors.Is/As compatibility.
func (e *ConfigError) Unwrap() error { return e.RawError }

// GroupedErrors walks the raw CUE error tree and returns errors grouped by
// message. Each GroupedError holds the message and all distinct source
// positions (primary + contributing) that report it, so conflicts between
// multiple files appear as a single entry with multiple locations.
//
// Returns nil if RawError is nil or produces no parseable errors.
func (e *ConfigError) GroupedErrors() []GroupedError {
	if e.RawError == nil {
		return nil
	}
	return groupCUEErrors(e.RawError)
}

// GroupedErrorsFromError attempts to extract CUE errors from any error
// (including wrapped ones such as fmt.Errorf("...: %w", cueErr)) and group
// them by message. This handles cases where CUE errors are wrapped before
// reaching the display layer.
//
// Returns nil if no CUE error information can be extracted.
func GroupedErrorsFromError(err error) []GroupedError {
	if err == nil {
		return nil
	}
	return groupCUEErrors(err)
}

// groupCUEErrors is the shared implementation for GroupedErrors and
// GroupedErrorsFromError. It walks the CUE error tree obtained from err and
// groups errors by message, collecting all source positions (primary +
// contributing via InputPositions) per group.
func groupCUEErrors(err error) []GroupedError {
	cueErrs := cueerrors.Errors(err)
	if len(cueErrs) == 0 {
		return nil
	}

	// groupOrder preserves insertion order of first-seen message/path pairs.
	type groupKey struct {
		msg  string
		path string
	}
	var groupOrder []groupKey
	groupMap := make(map[groupKey]*GroupedError)

	for _, ce := range cueErrs {
		path := normalizeCUEPath(ce.Path())

		format, args := ce.Msg()
		var msg string
		if len(args) == 0 {
			msg = format
		} else {
			msg = fmt.Sprintf(format, args...)
		}

		// Skip disjunction summary lines — they add noise without actionable info.
		if strings.Contains(msg, "errors in empty disjunction") {
			continue
		}

		key := groupKey{msg: msg, path: path}
		ge, exists := groupMap[key]
		if !exists {
			ge = &GroupedError{Message: msg}
			groupMap[key] = ge
			groupOrder = append(groupOrder, key)
		}

		// Collect all positions: primary + contributing (e.g. both sides of a
		// conflict). cueerrors.Positions returns Position() + InputPositions()
		// deduped and sorted.
		seen := make(map[string]bool, len(ge.Locations))
		for _, loc := range ge.Locations {
			seen[fmt.Sprintf("%s:%d:%d", loc.File, loc.Line, loc.Column)] = true
		}
		for _, pos := range cueerrors.Positions(ce) {
			if !pos.IsValid() {
				continue
			}
			file := filepath.Base(pos.Filename())
			locKey := fmt.Sprintf("%s:%d:%d", file, pos.Line(), pos.Column())
			if seen[locKey] {
				continue
			}
			seen[locKey] = true
			ge.Locations = append(ge.Locations, ErrorLocation{
				File:   file,
				Line:   pos.Line(),
				Column: pos.Column(),
				Path:   path,
			})
		}

		// If no valid position existed, record a position-less location so the
		// error message is still surfaced.
		if len(ge.Locations) == 0 {
			ge.Locations = append(ge.Locations, ErrorLocation{Path: path})
		}
	}

	out := make([]GroupedError, 0, len(groupOrder))
	for _, key := range groupOrder {
		out = append(out, *groupMap[key])
	}
	return out
}

func normalizeCUEPath(parts []string) string {
	if len(parts) == 0 {
		return ""
	}

	path := strings.Join(parts, ".")
	path = strings.TrimPrefix(path, "#module.#config.")
	path = strings.TrimPrefix(path, "#module.#config")
	path = strings.TrimPrefix(path, "#config.")
	path = strings.TrimPrefix(path, "#config")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return "values"
	}
	if strings.HasPrefix(path, "values.") || path == "values" {
		return path
	}
	return "values." + path
}
