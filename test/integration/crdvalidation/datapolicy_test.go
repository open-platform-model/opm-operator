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
	"maps"
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

// spec.dataPolicy is an optional enum on ModuleInstance and ModulePackage:
// Keep and Delete are admitted, an object without the field is admitted and
// stored without it (the reconciler reads an absent value as Keep), Delete
// without spec.prune is admitted, and every other value, an explicit empty
// string included, is refused by the API server.
func TestDataPolicyEnumAtAdmission(t *testing.T) {
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

	kinds := map[string]map[string]any{
		"ModuleInstance": {
			"module": map[string]any{"path": "example.com/modules/hello@v0", "version": "0.1.0"},
		},
		"ModulePackage": {
			"path":      "releases/hello",
			"sourceRef": map[string]any{"kind": "OCIRepository", "name": "hello"},
		},
	}

	for kind, baseSpec := range kinds {
		object := func(name string, extra map[string]any) *unstructured.Unstructured {
			spec := map[string]any{}
			maps.Copy(spec, baseSpec)
			maps.Copy(spec, extra)
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion(releasesv1alpha1.GroupVersion.String())
			obj.SetKind(kind)
			obj.SetNamespace("default")
			obj.SetName(name)
			obj.Object["spec"] = spec
			return obj
		}

		t.Run(kind+" without the field is admitted and stored without it", func(t *testing.T) {
			obj := object("absent", map[string]any{"prune": true})
			require.NoError(t, k8sClient.Create(ctx, obj))
			stored := obj.DeepCopy()
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored))
			_, found, err := unstructured.NestedFieldNoCopy(stored.Object, "spec", "dataPolicy")
			require.NoError(t, err)
			require.False(t, found, "the API server must apply no default")
		})

		for name, policy := range map[string]releasesv1alpha1.DataPolicy{
			"policy-keep":   releasesv1alpha1.DataPolicyKeep,
			"policy-delete": releasesv1alpha1.DataPolicyDelete,
		} {
			t.Run(kind+" "+string(policy)+" is admitted", func(t *testing.T) {
				obj := object(name, map[string]any{"prune": true, "dataPolicy": string(policy)})
				require.NoError(t, k8sClient.Create(ctx, obj))
				got, _, err := unstructured.NestedString(obj.Object, "spec", "dataPolicy")
				require.NoError(t, err)
				require.Equal(t, string(policy), got)
			})
		}

		t.Run(kind+" Delete without prune is admitted", func(t *testing.T) {
			require.NoError(t, k8sClient.Create(ctx, object("no-prune", map[string]any{"dataPolicy": "Delete"})))
		})

		for name, value := range map[string]any{"unknown": "Purge", "lowercase": "delete", "empty": "", "boolean": true} {
			t.Run(kind+" "+name+" value is refused", func(t *testing.T) {
				err := k8sClient.Create(ctx, object("bad-"+name, map[string]any{"prune": true, "dataPolicy": value}))
				require.Error(t, err)
				require.True(t, apierrors.IsInvalid(err), "want an Invalid error, got %v", err)
			})
		}
	}
}
