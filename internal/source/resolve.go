package source

import (
	"context"
	"fmt"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// Source kinds supported by Resolve.
const (
	SourceKindOCIRepository = "OCIRepository"
	SourceKindGitRepository = "GitRepository"
	SourceKindBucket        = "Bucket"
)

// fluxSource is the subset of the Flux source-controller API used by Resolve:
// a runtime object with readiness conditions and an artifact pointer.
type fluxSource interface {
	client.Object
	GetArtifact() *fluxmeta.Artifact
	GetConditions() []metav1.Condition
}

// Resolve looks up the Flux source referenced by sourceRef, validates its
// readiness, and extracts artifact metadata. Supports OCIRepository,
// GitRepository, and Bucket source kinds. The source is looked up in
// releaseNamespace unless sourceRef.Namespace is set.
func Resolve(
	ctx context.Context,
	c client.Client,
	sourceRef releasesv1alpha1.SourceReference,
	releaseNamespace string,
) (*ArtifactRef, error) {
	ns := releaseNamespace
	if sourceRef.Namespace != "" {
		ns = sourceRef.Namespace
	}

	obj, err := newSourceObject(sourceRef.Kind)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: %w", ns, sourceRef.Name, err)
	}

	key := types.NamespacedName{Name: sourceRef.Name, Namespace: ns}
	if getErr := c.Get(ctx, key, obj); getErr != nil {
		if client.IgnoreNotFound(getErr) == nil {
			return nil, fmt.Errorf("%s %s/%s: %w", sourceRef.Kind, ns, sourceRef.Name, ErrSourceNotFound)
		}
		return nil, fmt.Errorf("getting %s %s/%s: %w", sourceRef.Kind, ns, sourceRef.Name, getErr)
	}

	ready := apimeta.FindStatusCondition(obj.GetConditions(), fluxmeta.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return nil, fmt.Errorf("%s %s/%s: %w", sourceRef.Kind, ns, sourceRef.Name, ErrSourceNotReady)
	}

	artifact := obj.GetArtifact()
	if artifact == nil {
		return nil, fmt.Errorf("%s %s/%s has no artifact: %w", sourceRef.Kind, ns, sourceRef.Name, ErrSourceNotReady)
	}

	return &ArtifactRef{
		Kind:     sourceRef.Kind,
		URL:      artifact.URL,
		Revision: artifact.Revision,
		Digest:   artifact.Digest,
	}, nil
}

// newSourceObject returns a zero-value source object matching the given kind.
// Returns an error wrapping ErrUnsupportedSourceKind for unknown kinds.
func newSourceObject(kind string) (fluxSource, error) {
	switch kind {
	case SourceKindOCIRepository:
		return &sourcev1.OCIRepository{}, nil
	case SourceKindGitRepository:
		return &sourcev1.GitRepository{}, nil
	case SourceKindBucket:
		return &sourcev1.Bucket{}, nil
	default:
		return nil, fmt.Errorf("source kind %q: %w", kind, ErrUnsupportedSourceKind)
	}
}
