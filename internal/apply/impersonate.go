package apply

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewImpersonatedClient builds a controller-runtime client that impersonates
// the given ServiceAccount for all API calls. The SA must exist in the
// specified namespace; if it does not, an error is returned so the caller
// can stall the reconcile.
//
// reader is used only for the SA existence check. Pass an uncached reader
// (e.g. manager.GetAPIReader()) so that a single Get does not provision a
// cluster-wide ServiceAccount informer and thereby require list/watch RBAC.
// scheme is used to build the impersonated client.
//
// The returned client is suitable for Apply and Prune operations scoped to
// the SA's RBAC bindings.
func NewImpersonatedClient(
	ctx context.Context,
	cfg *rest.Config,
	reader client.Reader,
	scheme *runtime.Scheme,
	namespace, saName string,
) (client.Client, error) {
	var sa corev1.ServiceAccount
	if err := reader.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      saName,
	}, &sa); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("serviceAccount %s/%s not found: %w", namespace, saName, err)
		}
		return nil, fmt.Errorf("checking serviceAccount %s/%s: %w", namespace, saName, err)
	}

	impCfg := rest.CopyConfig(cfg)
	impCfg.Impersonate = rest.ImpersonationConfig{
		UserName: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, saName),
	}

	impClient, err := client.New(impCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("building impersonated client for %s/%s: %w", namespace, saName, err)
	}

	return impClient, nil
}
