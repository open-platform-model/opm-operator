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

func TestMaySkip(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	interval := 30 * time.Minute
	key := completeKey()
	readyOK := []metav1.Condition{{Type: status.ReadyCondition, Status: metav1.ConditionTrue, Reason: status.ReconciliationSucceededReason}}

	type input struct {
		interval   time.Duration
		conditions []metav1.Condition
		gen, obs   int64
		recorded   *releasesv1alpha1.RenderInputs
		key        status.RenderInputKey
	}
	base := func() input {
		return input{
			interval:   interval,
			conditions: readyOK,
			gen:        3, obs: 3,
			recorded: &releasesv1alpha1.RenderInputs{Digest: key.Digest(), RenderedAt: metav1.NewTime(now.Add(-5 * time.Minute))},
			key:      key,
		}
	}
	cases := []struct {
		name   string
		mutate func(*input)
		want   bool
	}{
		{"all conditions hold", func(*input) {}, true},
		{"skip disabled", func(in *input) { in.interval = 0 }, false},
		{"nothing recorded", func(in *input) { in.recorded = nil }, false},
		{"render older than the interval", func(in *input) {
			in.recorded.RenderedAt = metav1.NewTime(now.Add(-31 * time.Minute))
		}, false},
		{"render exactly one interval ago", func(in *input) {
			in.recorded.RenderedAt = metav1.NewTime(now.Add(-interval))
		}, false},
		{"not ready", func(in *input) {
			in.conditions = []metav1.Condition{{Type: status.ReadyCondition, Status: metav1.ConditionFalse, Reason: status.ApplyFailedReason}}
		}, false},
		{"ready for another reason", func(in *input) {
			in.conditions = []metav1.Condition{{Type: status.ReadyCondition, Status: metav1.ConditionTrue, Reason: "Other"}}
		}, false},
		{"no Ready condition", func(in *input) { in.conditions = nil }, false},
		{"generation not observed", func(in *input) { in.gen = 4 }, false},
		{"key changed", func(in *input) { in.key.PackageIdentity = "gen-2" }, false},
		{"key incomplete", func(in *input) { in.key.LibraryVersion = "" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mutate(&in)
			s := renderSkip{interval: in.interval, now: now}
			assert.Equal(t, tc.want, s.maySkip(in.conditions, in.gen, in.obs, in.recorded, in.key))
		})
	}
}
