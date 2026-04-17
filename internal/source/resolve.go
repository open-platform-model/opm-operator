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

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

// Resolve looks up the OCIRepository referenced by sourceRef, validates its
// readiness, and extracts artifact metadata. The OCIRepository is looked up
// in releaseNamespace unless sourceRef.Namespace is set.
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

	var repo sourcev1.OCIRepository
	key := types.NamespacedName{Name: sourceRef.Name, Namespace: ns}
	if err := c.Get(ctx, key, &repo); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, fmt.Errorf("OCIRepository %s/%s: %w", ns, sourceRef.Name, ErrSourceNotFound)
		}
		return nil, fmt.Errorf("getting OCIRepository %s/%s: %w", ns, sourceRef.Name, err)
	}

	ready := apimeta.FindStatusCondition(repo.Status.Conditions, fluxmeta.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return nil, fmt.Errorf("OCIRepository %s/%s: %w", ns, sourceRef.Name, ErrSourceNotReady)
	}

	artifact := repo.GetArtifact()
	if artifact == nil {
		return nil, fmt.Errorf("OCIRepository %s/%s has no artifact: %w", ns, sourceRef.Name, ErrSourceNotReady)
	}

	return &ArtifactRef{
		URL:      artifact.URL,
		Revision: artifact.Revision,
		Digest:   artifact.Digest,
	}, nil
}
