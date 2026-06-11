package render

import (
	"errors"
)

// Kind constants surface the CUE `kind` field so the reconciler can dispatch
// to the appropriate pipeline without re-evaluating the value.
const (
	KindModuleRelease = "ModuleRelease"
	KindBundleRelease = "BundleRelease"
)

// ErrUnsupportedKind indicates the loaded CUE value has a `kind` field that
// this controller cannot render (e.g., BundleRelease, which is not yet
// implemented).
var ErrUnsupportedKind = errors.New("unsupported release kind")
