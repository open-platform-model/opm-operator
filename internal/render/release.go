package render

import (
	"errors"
)

// KindModuleRelease surfaces the CUE `kind` field for the only renderable
// release kind, so the reconciler can record it without re-evaluating the value.
const KindModuleRelease = "ModuleRelease"

// ErrUnsupportedKind indicates the loaded CUE value has a `kind` field that
// this controller cannot render. Only #ModuleRelease is renderable; any other
// kind is rejected with this error.
var ErrUnsupportedKind = errors.New("unsupported release kind")
