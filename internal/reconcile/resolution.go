package reconcile

import (
	"errors"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/k8s/object"

	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// isTypedResolutionError reports whether err carries one of the library's
// typed resolution-class failures: an identity mismatch from module acquire
// (oerrors.IdentityError, a value type returned bare), unresolved platform
// demands (*oerrors.UnresolvedDemandsError) or components no transformer
// matched (*oerrors.UnmatchedComponentsError). The last two are the typed
// causes of the kernel's fail-closed render gate, carried on
// *kernel.RenderError and joined together when both apply; errors.AsType
// traverses the join. Both render-error classifiers consult it through
// renderFailureReason, and isTerminalCause treats it as terminal.
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

// isTerminalCause reports whether err carries a typed cause that retrying
// cannot fix, in any phase of the render: a wrong artifact kind, a
// structurally invalid package or a missing required field (the loader's
// shape gate), or a typed resolution failure (an identity mismatch,
// unresolved platform demands, unmatched components). A registry fetch
// failure joined to one of these does not make the failure retry.
func isTerminalCause(err error) bool {
	if errors.Is(err, oerrors.ErrWrongKind) ||
		errors.Is(err, oerrors.ErrInvalidPackage) ||
		errors.Is(err, oerrors.ErrMissingRequiredField) {
		return true
	}
	return isTypedResolutionError(err)
}

// IsTransientFailure reports a failure a later attempt may get past with
// nothing changed: a registry fetch or dependency resolution failure the
// library typed (*oerrors.FetchError, any Kind: unreachable, not found,
// unauthorized or other, 0021:D8:R12), with no typed terminal cause in the
// chain. It holds in every phase of a render (module acquisition, values
// compile, instance synthesis, the render build, the package load), because
// the library classifies at each site where a registry interaction leaves
// it. Every fetch failure retries, not only ErrTransient: owner decision a3
// (2026-10-02/03 walkthrough) set that a fetch failure must not stall for 30
// minutes. Anything the library left unclassified (a CUE syntax error, a
// values conflict, an unparsable version) is an author defect and is not
// transient. A raw context.DeadlineExceeded is not either: the reconcile
// context carries no deadline, and the library already classifies a deadline
// at a fetch site. It reads types only, never message text.
//
// Both reconcile loops retry a transient failure on the bounded backoff as a
// non-stalled ResolutionFailed, and the Platform reconciler picks its short
// recheck interval with it.
func IsTransientFailure(err error) bool {
	if err == nil || isTerminalCause(err) {
		return false
	}
	_, ok := errors.AsType[*oerrors.FetchError](err)
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
// (*object.DuplicateIdentitiesError, 0015:D15). The render adapter
// returns it bare, and errors.AsType still finds it through a wrap.
func isDuplicateIdentities(err error) bool {
	_, ok := errors.AsType[*object.DuplicateIdentitiesError](err)
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
//  3. ResolutionFailed — unresolved demands, unmatched components, identity
//     mismatches and every acquisition failure (render.ErrAcquire). The
//     callers route a transient failure (IsTransientFailure) before reaching
//     here, so under this reason a stalled acquisition failure reads
//     ResolutionFailed: a typed terminal cause, or a module acquisition or
//     package load the library did not classify as a registry fetch.
//  4. RenderFailed — a transform failure, an over-subscribed provider
//     contract (*oerrors.TransformError,
//     *oerrors.OverSubscribedContractsError) and every other refusal or
//     evaluation error. The pre-evaluation refusals that indicate an operator
//     defect (a missing Source, an uncovered OPM path) fall through to here
//     with the kernel's message verbatim. A failed instance synthesis lands
//     here too, unless it is a registry fetch failure (transient, routed
//     before here).
//
// There is no string fallback: a render error is classified by its type or
// sentinel, never by its message text.
func renderFailureReason(err error) string {
	switch {
	case isSkewRefusal(err):
		return status.SkewRefusedReason
	case isDuplicateIdentities(err):
		return status.DuplicateIdentitiesReason
	case isTypedResolutionError(err), errors.Is(err, render.ErrAcquire):
		return status.ResolutionFailedReason
	default:
		return status.RenderFailedReason
	}
}
