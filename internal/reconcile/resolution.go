package reconcile

import (
	"errors"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/helper/objectset"

	"github.com/open-platform-model/opm-operator/internal/status"
)

// isTypedResolutionError reports whether err carries one of the library's
// typed resolution-class failures: an identity mismatch from module acquire
// (oerrors.IdentityError, a value type returned bare), unresolved platform
// demands (*oerrors.UnresolvedDemandsError) or components no transformer
// matched (*oerrors.UnmatchedComponentsError). The last two are the typed
// causes of the kernel's fail-closed render gate, carried on
// *kernel.RenderError and joined together when both apply; errors.AsType
// traverses the join. Both render-error classifiers consult this ahead of
// their string fallbacks, which remain for loader-path errors that carry no
// type.
//
// IdentityError cannot occur on the ModulePackage path — packages load from
// a Flux artifact and never acquire from the registry — but the helper is
// shared unchanged so the two paths cannot drift.
func isTypedResolutionError(err error) bool {
	if _, ok := errors.AsType[oerrors.IdentityError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*oerrors.UnresolvedDemandsError](err); ok {
		return true
	}
	_, ok := errors.AsType[*oerrors.UnmatchedComponentsError](err)
	return ok
}

// isSkewRefusal reports whether err is a render refused before evaluation by
// the Refuse skew policy (*oerrors.SkewError, 0019:D7/D18). The
// kernel joins one SkewError per skewed path; the first is enough to classify.
func isSkewRefusal(err error) bool {
	_, ok := errors.AsType[*oerrors.SkewError](err)
	return ok
}

// isDuplicateIdentities reports whether err is the adapter's refusal of a
// render whose objects share one Kubernetes apply identity
// (*objectset.DuplicateIdentitiesError, 0015:D15). The render adapter
// returns it bare, and errors.AsType still finds it through a wrap.
func isDuplicateIdentities(err error) bool {
	_, ok := errors.AsType[*objectset.DuplicateIdentitiesError](err)
	return ok
}

// renderFailureReason maps a failed render to its Ready-condition reason by
// the typed cause, in precedence order:
//
//  1. SkewRefused — the Refuse skew policy stopped the render before
//     evaluation, so nothing was rendered.
//  2. DuplicateIdentities — two rendered objects share one apply identity, a
//     verdict on the render's own output that must not be mistaken for a
//     platform problem.
//  3. ResolutionFailed — unresolved demands, unmatched components and
//     identity mismatches.
//  4. RenderFailed — a transform failure, an over-subscribed provider
//     contract (*oerrors.TransformError,
//     *oerrors.OverSubscribedContractsError) and every other refusal or
//     evaluation error. The pre-evaluation refusals that indicate an operator
//     defect (a missing Source, an uncovered OPM path) fall through to here
//     with the kernel's message verbatim.
//
// A string fallback classifies loader-path errors that carry no type
// (matchers supplied by the caller: the two reconcile loops wrap different
// phases in different words).
func renderFailureReason(err error, isResolutionMsg func(error) bool) string {
	switch {
	case isSkewRefusal(err):
		return status.SkewRefusedReason
	case isDuplicateIdentities(err):
		return status.DuplicateIdentitiesReason
	case isTypedResolutionError(err), isResolutionMsg(err):
		return status.ResolutionFailedReason
	default:
		return status.RenderFailedReason
	}
}
