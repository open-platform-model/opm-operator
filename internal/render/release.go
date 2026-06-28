package render

import (
	"errors"
)

// KindModuleInstance surfaces the CUE `kind` field for the only renderable
// instance kind, so the reconciler can record it without re-evaluating the value.
//
// Was: KindModuleRelease = "ModuleRelease"
const KindModuleInstance = "ModuleInstance"

// ErrUnsupportedKind indicates the loaded CUE value has a `kind` field that
// this controller cannot render. Only #ModuleInstance is renderable; any other
// kind is rejected with this error.
var ErrUnsupportedKind = errors.New("unsupported release kind")
