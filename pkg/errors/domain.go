package errors

import (
	"fmt"
)

// TransformError indicates transformer execution failed.
type TransformError struct {
	ComponentName  string
	TransformerFQN string
	Cause          error
}

func (e *TransformError) Error() string {
	return fmt.Sprintf("component %q, transformer %q: %v",
		e.ComponentName, e.TransformerFQN, e.Cause)
}

func (e *TransformError) Unwrap() error {
	return e.Cause
}

// Component returns the component name where the error occurred.
func (e *TransformError) Component() string {
	return e.ComponentName
}

// ValidationError indicates the release failed validation.
type ValidationError struct {
	// Message describes what validation failed.
	Message string

	// Cause is the underlying error.
	Cause error

	// Details contains the formatted CUE error output.
	Details string
}

func (e *ValidationError) Error() string {
	if e.Cause != nil {
		return "release validation failed: " + e.Message + ": " + e.Cause.Error()
	}
	return "release validation failed: " + e.Message
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
}

// FieldError is a single validation error tied to a specific source location
// in a values file.
type FieldError struct {
	// File is the values file name where the error occurred.
	File string

	// Line is the 1-based line number in File.
	Line int

	// Column is the 1-based column number in File.
	Column int

	// Path is the dot-joined field path from the values root (e.g. "values.db.port").
	Path string

	// Message is the human-readable error description.
	Message string
}

// ErrorLocation is a source position paired with its CUE field path.
// Used in GroupedError to record every location where an error message appears.
type ErrorLocation struct {
	// File is the values file name (basename only).
	File string

	// Line is the 1-based line number. Zero means no position information.
	Line int

	// Column is the 1-based column number.
	Column int

	// Path is the dot-joined CUE field path at this position (e.g. "values.db.port").
	// May be empty when the error has no associated path.
	Path string
}

// GroupedError collects all source locations where the same error message
// appears. CUE can report the same logical error at multiple positions (e.g.
// both sides of a value conflict), so grouping by message collapses duplicates
// and makes conflicts immediately readable without a separate section.
type GroupedError struct {
	// Message is the human-readable error description shared by all locations.
	Message string

	// Locations holds one entry per distinct source position reporting this error.
	Locations []ErrorLocation
}
