/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crdvalidation_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// TransformerRegistration is validated at admission rather than by the
// authoring catalog (change transformer-registration-crd). CUE reports a
// missing required field as an incomplete value, not an error, so a claim can
// reach the cluster with a field absent even though the contract declares it
// required; the CRD carries its own constraints instead of trusting the
// renderer.
//
// The objects below are built unstructured on purpose. A typed client would
// supply the group, version and kind from the Go type and prove nothing about
// them, and it could not omit a required field: a zero-valued string
// serializes as "" and trips MinLength, which is a different refusal from the
// one a missing key produces. Unstructured literals assert exactly what the
// catalog_opm renderer emits.
func TestTransformerRegistrationAtAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if dir := firstEnvtestBinaryDir(t); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testEnv.Stop())
	})
	require.NoError(t, releasesv1alpha1.AddToScheme(scheme.Scheme))
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	// The shape the catalog_opm renderer emits, recorded in opm-operator issue
	// 132. The apiVersion is this repo's flat group: the issue's JSON recorded
	// the prefixed "opm.opmodel.dev" spelling, which catalog_opm corrected to
	// this one in its correct-registration-api-group change (PR 91).
	// Everything else is the issue's JSON verbatim, so drift between the two
	// sides of the contract fails a test rather than a cluster.
	rendered := func() *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "opmodel.dev/v1alpha1",
			"kind":       "TransformerRegistration",
			"metadata": map[string]any{
				"name": "backup-system.k8up",
				"labels": map[string]any{
					"app.kubernetes.io/name":           "k8up",
					"app.kubernetes.io/instance":       "k8up",
					"app.kubernetes.io/managed-by":     "opm-test",
					"module-instance.opmodel.dev/name": "k8up",
				},
			},
			"spec": map[string]any{
				"catalog":  "opmodel.dev/catalogs/k8up@v1",
				"version":  "1.0.0",
				"provides": []any{"opmodel.dev/catalogs/opm/traits/backup@v1alpha1"},
				"providerRef": map[string]any{
					"name":      "k8up",
					"namespace": "backup-system",
				},
			},
		}}
	}

	t.Run("the rendered claim is accepted", func(t *testing.T) {
		claim := rendered()
		require.NoError(t, k8sClient.Create(ctx, claim))
		require.Empty(t, claim.GetNamespace(), "the kind is cluster-scoped")
		t.Cleanup(func() { require.NoError(t, k8sClient.Delete(ctx, claim)) })
	})

	t.Run("a claim missing its catalog is rejected naming the field", func(t *testing.T) {
		claim := rendered()
		claim.SetName("backup-system.nocatalog")
		spec, _, err := unstructured.NestedMap(claim.Object, "spec")
		require.NoError(t, err)
		delete(spec, "catalog")
		require.NoError(t, unstructured.SetNestedMap(claim.Object, spec, "spec"))

		err = k8sClient.Create(ctx, claim)
		require.Error(t, err)
		require.True(t, apierrors.IsInvalid(err), "got %v", err)
		require.Contains(t, err.Error(), "spec.catalog",
			"the refusal must name the missing field, not just fail")
	})

	t.Run("a name carrying no dot is rejected by the CEL rule", func(t *testing.T) {
		claim := rendered()
		claim.SetName("k8up")

		err := k8sClient.Create(ctx, claim)
		require.Error(t, err)
		require.True(t, apierrors.IsInvalid(err), "got %v", err)
		require.Contains(t, err.Error(),
			"name must be the dot-joined <namespace>.<name> of the claiming instance",
			"the refusal must identify the required name shape")
	})

	// Cluster scope is asserted by what the API server STORES, not by a
	// refusal. Measured against envtest at Kubernetes 1.36: a claim submitted
	// with metadata.namespace is accepted and the namespace is silently
	// stripped, because rest.BeforeCreate clears it on a cluster-scoped kind
	// before validation runs. There is no rejection to assert here; a test
	// expecting one would be testing a Kubernetes behavior that does not
	// exist.
	t.Run("a claim submitted with a namespace is stored without one", func(t *testing.T) {
		claim := rendered()
		claim.SetName("backup-system.namespaced")
		claim.SetNamespace("default")

		require.NoError(t, k8sClient.Create(ctx, claim))
		require.Empty(t, claim.GetNamespace(),
			"a cluster-scoped kind stores no namespace, whatever the client sent")
		t.Cleanup(func() { require.NoError(t, k8sClient.Delete(ctx, claim)) })
	})

	// An empty provides list is ACCEPTED, deliberately. A provider catalog
	// implementing no provider-fulfilled contract is a claim acceptance
	// refuses on its merits, naming the catalog it re-derived from; the CRD is
	// the wrong place to encode a rule the reconciler states better. Do not
	// "fix" this into a MinItems constraint.
	t.Run("an empty provides list is accepted", func(t *testing.T) {
		claim := rendered()
		claim.SetName("backup-system.noprovides")
		require.NoError(t, unstructured.SetNestedSlice(claim.Object, []any{}, "spec", "provides"))

		require.NoError(t, k8sClient.Create(ctx, claim))
		t.Cleanup(func() { require.NoError(t, k8sClient.Delete(ctx, claim)) })
	})
}
