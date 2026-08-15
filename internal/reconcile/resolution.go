package reconcile

import (
	"errors"

	oerrors "github.com/open-platform-model/library/opm/errors"
)

// isTypedResolutionError reports whether err carries one of the library's
// typed resolution-class failures: an identity mismatch from module acquire
// (oerrors.IdentityError, a value type returned bare) or unresolved platform
// demands from kernel compile (*oerrors.UnresolvedDemandsError, possibly
// joined with *compile.UnmatchedComponentsError — errors.AsType traverses
// the join). Both render-error classifiers consult this ahead of their
// string fallbacks, which remain for loader-path errors that carry no type.
//
// IdentityError cannot occur on the ModulePackage path — packages load from
// a Flux artifact and never acquire from the registry — but the helper is
// shared unchanged so the two paths cannot drift.
func isTypedResolutionError(err error) bool {
	if _, ok := errors.AsType[oerrors.IdentityError](err); ok {
		return true
	}
	_, ok := errors.AsType[*oerrors.UnresolvedDemandsError](err)
	return ok
}
