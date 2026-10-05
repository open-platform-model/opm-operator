package reconcile

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// renderedKey is the render input key of a render that has run: the source
// and config digests the reconcile computed, the platform package identity
// and skew policy the render itself reports (the platform record it leased,
// never what was read before it), and the running operator and library
// versions.
func renderedKey(source, config string, result *render.RenderResult, operatorVersion, libraryVersion string) status.RenderInputKey {
	return status.RenderInputKey{
		Source:          source,
		Config:          config,
		PackageIdentity: result.PlatformIdentity,
		SkewPolicy:      result.SkewPolicy,
		OperatorVersion: operatorVersion,
		LibraryVersion:  libraryVersion,
	}
}

// recordInputs writes status.lastAppliedInputs from key, the key of this
// attempt's render. A nil key means this attempt did not render, and the
// field is left as it was. An incomplete key clears the field, so a stale key
// never survives a render whose inputs could not be named. A complete key is
// recorded with now as its render time.
func recordInputs(field **releasesv1alpha1.RenderInputs, key *status.RenderInputKey, now metav1.Time) {
	if key == nil {
		return
	}
	if !key.Complete() {
		*field = nil
		return
	}
	*field = &releasesv1alpha1.RenderInputs{Digest: key.Digest(), RenderedAt: now}
}

// noOpInputs is the key a NoOp records: the render's key while the skip is
// enabled, nil (record nothing) when interval is zero. With the skip disabled
// nothing reads lastAppliedInputs, and moving renderedAt would turn every
// NoOp into a status write.
func noOpInputs(interval time.Duration, key *status.RenderInputKey) *status.RenderInputKey {
	if interval <= 0 {
		return nil
	}
	return key
}

// platformKeyParts reads the platform parts of the pre-render key from the
// cluster Platform: its pin set ([platformstore.PinSet]), and the resolved
// spec.skewPolicy in its API spelling. It reads the CR rather than the
// platform store because the CR survives an operator restart: an unchanged
// object can skip while the store is still empty. A missing Platform or any
// read error returns empty parts, which make the key incomplete, so the
// reconcile renders.
func platformKeyParts(ctx context.Context, c client.Reader) (identity, skew string) {
	var plat releasesv1alpha1.Platform
	if err := c.Get(ctx, client.ObjectKey{Name: platformstore.SingletonName}, &plat); err != nil {
		if !apierrors.IsNotFound(err) {
			logf.FromContext(ctx).V(1).Info("Reading the Platform for the render input key failed; rendering", "error", err.Error())
		}
		return "", ""
	}
	return platformstore.PinSet(&plat), platformstore.SkewPolicyName(platformstore.ResolveSkewPolicy(&plat))
}

// renderSkip decides whether a reconcile may skip its render.
type renderSkip struct {
	// interval is the drift render interval; zero or less never skips.
	interval time.Duration
	now      time.Time
}

// maySkip reports whether a reconcile may skip its render: only when the
// skip is enabled, the last confirming render (recorded) is younger than the
// interval, the object is Ready with reason ReconciliationSucceeded, it has
// observed its generation, and the pre-render key is complete and matches
// the recorded one. The checks run cheapest first, and key, which reads the
// Platform, is called only once every other check has passed. Each guards
// against a skip hiding something a render would find: a stale render (the
// interval), a failed or refused attempt (Ready), a spec edit outside the key
// (the generation), and an input change (the key). A renderedAt in the future
// (a clock that was ahead, or a hand-written status) counts as expired, so
// the interval stays an upper bound on how long a skip lasts.
func (s renderSkip) maySkip(
	conditions []metav1.Condition,
	generation, observedGeneration int64,
	recorded *releasesv1alpha1.RenderInputs,
	key func() status.RenderInputKey,
) bool {
	if s.interval <= 0 || recorded == nil {
		return false
	}
	if age := s.now.Sub(recorded.RenderedAt.Time); age < 0 || age >= s.interval {
		return false
	}
	ready := apimeta.FindStatusCondition(conditions, status.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.Reason != status.ReconciliationSucceededReason {
		return false
	}
	if generation != observedGeneration {
		return false
	}
	k := key()
	return k.Complete() && k.Digest() == recorded.Digest
}

// logRenderSkip records a skipped render: one line, with the time from which
// a reconcile renders again whatever its inputs.
func logRenderSkip(ctx context.Context, recorded *releasesv1alpha1.RenderInputs, interval time.Duration) {
	logf.FromContext(ctx).Info("Render inputs unchanged, skipping render",
		"renderedAt", recorded.RenderedAt.Time,
		"rendersAgainAfter", recorded.RenderedAt.Add(interval))
}
