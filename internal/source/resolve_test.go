package source

import (
	"context"
	"errors"
	"testing"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = sourcev1.AddToScheme(s)
	return s
}

func readyCondition(status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{
		Type:   "Ready",
		Status: status,
	}
}

func TestResolve(t *testing.T) {
	const (
		ns       = "default"
		repoName = "my-repo"
		url      = "http://source-controller/artifact.tar.gz"
		revision = "v0.1.0@sha256:abc123"
		digest   = "sha256:abc123"
	)

	sourceRef := releasesv1alpha1.SourceReference{
		Kind: "OCIRepository",
		Name: repoName,
	}

	readyRepo := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      repoName,
			Namespace: ns,
		},
		Status: sourcev1.OCIRepositoryStatus{
			Conditions: []metav1.Condition{readyCondition(metav1.ConditionTrue)},
			Artifact: &fluxmeta.Artifact{
				URL:      url,
				Revision: revision,
				Digest:   digest,
				Path:     "ocirepository/default/my-repo/sha256:abc123.tar.gz",
			},
		},
	}

	tests := []struct {
		name         string
		objects      []runtime.Object
		sourceRef    releasesv1alpha1.SourceReference
		releaseNS    string
		wantErr      error
		wantArtifact *ArtifactRef
	}{
		{
			name:      "source found and ready",
			objects:   []runtime.Object{readyRepo},
			sourceRef: sourceRef,
			releaseNS: ns,
			wantArtifact: &ArtifactRef{
				URL:      url,
				Revision: revision,
				Digest:   digest,
			},
		},
		{
			name:      "source not found",
			objects:   []runtime.Object{},
			sourceRef: sourceRef,
			releaseNS: ns,
			wantErr:   ErrSourceNotFound,
		},
		{
			name: "source not ready (Ready=False)",
			objects: []runtime.Object{&sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: ns},
				Status: sourcev1.OCIRepositoryStatus{
					Conditions: []metav1.Condition{readyCondition(metav1.ConditionFalse)},
				},
			}},
			sourceRef: sourceRef,
			releaseNS: ns,
			wantErr:   ErrSourceNotReady,
		},
		{
			name: "source not ready (Ready=Unknown)",
			objects: []runtime.Object{&sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: ns},
				Status: sourcev1.OCIRepositoryStatus{
					Conditions: []metav1.Condition{readyCondition(metav1.ConditionUnknown)},
				},
			}},
			sourceRef: sourceRef,
			releaseNS: ns,
			wantErr:   ErrSourceNotReady,
		},
		{
			name: "source ready but nil artifact",
			objects: []runtime.Object{&sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: ns},
				Status: sourcev1.OCIRepositoryStatus{
					Conditions: []metav1.Condition{readyCondition(metav1.ConditionTrue)},
				},
			}},
			sourceRef: sourceRef,
			releaseNS: ns,
			wantErr:   ErrSourceNotReady,
		},
		{
			name:    "empty sourceRef.Namespace uses releaseNamespace",
			objects: []runtime.Object{readyRepo},
			sourceRef: releasesv1alpha1.SourceReference{
				Kind: "OCIRepository",
				Name: repoName,
			},
			releaseNS: ns,
			wantArtifact: &ArtifactRef{
				URL:      url,
				Revision: revision,
				Digest:   digest,
			},
		},
		{
			name: "set sourceRef.Namespace overrides releaseNamespace",
			objects: []runtime.Object{&sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: "other-ns"},
				Status: sourcev1.OCIRepositoryStatus{
					Conditions: []metav1.Condition{readyCondition(metav1.ConditionTrue)},
					Artifact: &fluxmeta.Artifact{
						URL:      url,
						Revision: revision,
						Digest:   digest,
						Path:     "ocirepository/other-ns/my-repo/sha256:abc123.tar.gz",
					},
				},
			}},
			sourceRef: releasesv1alpha1.SourceReference{
				Kind:      "OCIRepository",
				Name:      repoName,
				Namespace: "other-ns",
			},
			releaseNS: ns,
			wantArtifact: &ArtifactRef{
				URL:      url,
				Revision: revision,
				Digest:   digest,
			},
		},
		{
			name: "GitRepository found and ready",
			objects: []runtime.Object{&sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: ns},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{readyCondition(metav1.ConditionTrue)},
					Artifact: &fluxmeta.Artifact{
						URL:      url,
						Revision: revision,
						Digest:   digest,
					},
				},
			}},
			sourceRef: releasesv1alpha1.SourceReference{
				Kind: "GitRepository",
				Name: repoName,
			},
			releaseNS: ns,
			wantArtifact: &ArtifactRef{
				URL:      url,
				Revision: revision,
				Digest:   digest,
			},
		},
		{
			name: "GitRepository not ready",
			objects: []runtime.Object{&sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: ns},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{readyCondition(metav1.ConditionFalse)},
				},
			}},
			sourceRef: releasesv1alpha1.SourceReference{Kind: "GitRepository", Name: repoName},
			releaseNS: ns,
			wantErr:   ErrSourceNotReady,
		},
		{
			name:    "GitRepository not found",
			objects: []runtime.Object{},
			sourceRef: releasesv1alpha1.SourceReference{
				Kind: "GitRepository",
				Name: repoName,
			},
			releaseNS: ns,
			wantErr:   ErrSourceNotFound,
		},
		{
			name: "Bucket found and ready",
			objects: []runtime.Object{&sourcev1.Bucket{
				ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: ns},
				Status: sourcev1.BucketStatus{
					Conditions: []metav1.Condition{readyCondition(metav1.ConditionTrue)},
					Artifact: &fluxmeta.Artifact{
						URL:      url,
						Revision: revision,
						Digest:   digest,
					},
				},
			}},
			sourceRef: releasesv1alpha1.SourceReference{
				Kind: "Bucket",
				Name: repoName,
			},
			releaseNS: ns,
			wantArtifact: &ArtifactRef{
				URL:      url,
				Revision: revision,
				Digest:   digest,
			},
		},
		{
			name:      "unsupported source kind",
			objects:   []runtime.Object{},
			sourceRef: releasesv1alpha1.SourceReference{Kind: "HelmRepository", Name: repoName},
			releaseNS: ns,
			wantErr:   ErrUnsupportedSourceKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(newScheme()).
				WithRuntimeObjects(tt.objects...).
				Build()

			got, err := Resolve(context.Background(), c, tt.sourceRef, tt.releaseNS)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error wrapping %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected errors.Is(%v, %v) = true, got false", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.URL != tt.wantArtifact.URL {
				t.Errorf("URL = %q, want %q", got.URL, tt.wantArtifact.URL)
			}
			if got.Revision != tt.wantArtifact.Revision {
				t.Errorf("Revision = %q, want %q", got.Revision, tt.wantArtifact.Revision)
			}
			if got.Digest != tt.wantArtifact.Digest {
				t.Errorf("Digest = %q, want %q", got.Digest, tt.wantArtifact.Digest)
			}
		})
	}
}
