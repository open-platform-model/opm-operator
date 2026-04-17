package errors

import "errors"

// Sentinel errors for known conditions.
var (
	// ErrValidation indicates a CUE schema validation failure.
	ErrValidation = errors.New("validation error")

	// ErrConnectivity indicates a network connectivity issue.
	ErrConnectivity = errors.New("connectivity error")

	// ErrPermission indicates insufficient permissions.
	ErrPermission = errors.New("permission denied")

	// ErrNotFound indicates a resource, module, or file was not found.
	ErrNotFound = errors.New("not found")
)
