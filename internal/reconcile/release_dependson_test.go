package reconcile

import (
	"context"
	"strings"
	"testing"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/status"
)

func dependsOnScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = releasesv1alpha1.AddToScheme(s)
	return s
}

func readyReleaseFixture(name string, ready bool) *releasesv1alpha1.Release {
	s := metav1.ConditionFalse
	if ready {
		s = metav1.ConditionTrue
	}
	return &releasesv1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status: releasesv1alpha1.ReleaseStatus{
			Conditions: []metav1.Condition{{
				Type:               status.ReadyCondition,
				Status:             s,
				LastTransitionTime: metav1.Now(),
				Reason:             "Test",
			}},
		},
	}
}

func TestCheckDependsOn(t *testing.T) {
	const ns = "default"
	_ = ns

	tests := []struct {
		name        string
		dependsOn   []fluxmeta.NamespacedObjectReference
		objects     []runtime.Object
		wantBlocker string // substring to look for
		wantErr     string
	}{
		{
			name:      "no dependencies",
			dependsOn: nil,
		},
		{
			name:      "empty dependencies slice",
			dependsOn: []fluxmeta.NamespacedObjectReference{},
		},
		{
			name:      "all dependencies ready",
			dependsOn: []fluxmeta.NamespacedObjectReference{{Name: "dep-a"}, {Name: "dep-b"}},
			objects: []runtime.Object{
				readyReleaseFixture("dep-a", true),
				readyReleaseFixture("dep-b", true),
			},
		},
		{
			name:      "one dependency not ready",
			dependsOn: []fluxmeta.NamespacedObjectReference{{Name: "dep-a"}, {Name: "dep-b"}},
			objects: []runtime.Object{
				readyReleaseFixture("dep-a", true),
				readyReleaseFixture("dep-b", false),
			},
			wantBlocker: "dep-b",
		},
		{
			name:      "dependency missing Ready condition",
			dependsOn: []fluxmeta.NamespacedObjectReference{{Name: "dep-pending"}},
			objects: []runtime.Object{&releasesv1alpha1.Release{
				ObjectMeta: metav1.ObjectMeta{Name: "dep-pending", Namespace: ns},
			}},
			wantBlocker: "dep-pending",
		},
		{
			name:        "dependency does not exist",
			dependsOn:   []fluxmeta.NamespacedObjectReference{{Name: "missing-dep"}},
			objects:     []runtime.Object{},
			wantBlocker: "missing-dep",
		},
		{
			name:      "cross-namespace dependency rejected",
			dependsOn: []fluxmeta.NamespacedObjectReference{{Name: "dep-a", Namespace: "other-ns"}},
			wantErr:   "cross-namespace",
		},
		{
			name:      "same-namespace dependency with explicit namespace",
			dependsOn: []fluxmeta.NamespacedObjectReference{{Name: "dep-a", Namespace: ns}},
			objects:   []runtime.Object{readyReleaseFixture("dep-a", true)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(dependsOnScheme()).WithRuntimeObjects(tt.objects...).Build()
			rel := &releasesv1alpha1.Release{
				ObjectMeta: metav1.ObjectMeta{Name: "self", Namespace: ns},
				Spec:       releasesv1alpha1.ReleaseSpec{DependsOn: tt.dependsOn},
			}

			blocker, err := checkDependsOn(context.Background(), c, rel)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantBlocker == "" {
				if blocker != "" {
					t.Fatalf("expected no blocker, got %q", blocker)
				}
				return
			}
			if !strings.Contains(blocker, tt.wantBlocker) {
				t.Fatalf("expected blocker containing %q, got %q", tt.wantBlocker, blocker)
			}
		})
	}
}
