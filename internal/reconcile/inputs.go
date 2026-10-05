package reconcile

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
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
