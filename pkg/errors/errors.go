// Package errors provides sentinel errors and structured error types for OPM.
package errors

import (
	"fmt"
	"strings"
)

// DetailError captures structured error information.
type DetailError struct {
	// Type is the error category (required).
	Type string

	// Message is the specific description (required).
	Message string

	// Location is the file path and line number (optional).
	Location string

	// Field is the field name for schema errors (optional).
	Field string

	// Context contains additional key-value context (optional).
	Context map[string]string

	// Hint provides actionable guidance (optional).
	Hint string

	// Cause is the underlying error (optional).
	Cause error
}

// Error implements the error interface.
func (e *DetailError) Error() string {
	var b strings.Builder

	b.WriteString("Error: ")
	b.WriteString(e.Type)
	b.WriteString("\n")

	if e.Location != "" {
		b.WriteString("  Location: ")
		b.WriteString(e.Location)
		b.WriteString("\n")
	}
	if e.Field != "" {
		b.WriteString("  Field: ")
		b.WriteString(e.Field)
		b.WriteString("\n")
	}
	for k, v := range e.Context {
		b.WriteString("  ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\n")
	}

	b.WriteString("\n  ")
	b.WriteString(e.Message)
	b.WriteString("\n")

	if e.Hint != "" {
		b.WriteString("\nHint: ")
		b.WriteString(e.Hint)
		b.WriteString("\n")
	}

	return b.String()
}

// Unwrap returns the underlying error.
func (e *DetailError) Unwrap() error {
	return e.Cause
}

// NewValidationError creates a validation error with details.
func NewValidationError(message, location, field, hint string) error {
	return &DetailError{
		Type:     "validation failed",
		Message:  message,
		Location: location,
		Field:    field,
		Hint:     hint,
		Cause:    ErrValidation,
	}
}

// Wrap wraps an error with a sentinel error type.
func Wrap(sentinel error, message string) error {
	return fmt.Errorf("%s: %w", message, sentinel)
}
