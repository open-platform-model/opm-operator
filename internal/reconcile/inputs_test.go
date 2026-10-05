package reconcile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

func completeKey() status.RenderInputKey {
	return status.RenderInputKey{
		Source: "sha256:src", Config: "sha256:cfg", PackageIdentity: "gen-1",
		SkewPolicy: "Warn", OperatorVersion: "v1.0.0", LibraryVersion: "v1.0.0-beta.4",
	}
}

func TestRecordInputs(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	prev := &releasesv1alpha1.RenderInputs{Digest: "sha256:prev", RenderedAt: metav1.NewTime(now.Add(-time.Hour))}

	t.Run("a nil key leaves the field", func(t *testing.T) {
		field := prev
		recordInputs(&field, nil, now)
		assert.Same(t, prev, field)
	})
	t.Run("an incomplete key clears the field", func(t *testing.T) {
		field := prev
		k := completeKey()
		k.PackageIdentity = ""
		recordInputs(&field, &k, now)
		assert.Nil(t, field)
	})
	t.Run("a complete key writes digest and time", func(t *testing.T) {
		field := prev
		k := completeKey()
		recordInputs(&field, &k, now)
		assert.Equal(t, &releasesv1alpha1.RenderInputs{Digest: k.Digest(), RenderedAt: now}, field)
	})
}

func TestRenderedKeyTakesThePlatformFromTheRender(t *testing.T) {
	result := &render.RenderResult{PlatformIdentity: "gen-4", SkewPolicy: "Refuse"}
	k := renderedKey("sha256:src", "sha256:cfg", result, "v1.0.0", "v1.0.0-beta.4")
	assert.Equal(t, status.RenderInputKey{
		Source: "sha256:src", Config: "sha256:cfg", PackageIdentity: "gen-4",
		SkewPolicy: "Refuse", OperatorVersion: "v1.0.0", LibraryVersion: "v1.0.0-beta.4",
	}, k)
}

func TestNoOpInputs(t *testing.T) {
	k := completeKey()
	assert.Nil(t, noOpInputs(0, &k), "a disabled skip records nothing on a NoOp")
	assert.Same(t, &k, noOpInputs(time.Minute, &k))
}
