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

// Package crdvalidation_test measures apiextensions validation semantics the
// operator's CRD posture depends on, against the same API server version the
// rest of the integration tier runs on.
package crdvalidation_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// The measurement behind the Subscription.Version CRD marker (change
// operator-library-retarget, task 1.1): the stored `cluster` Platform
// singleton predates the filter→version reshape (carries `filter`, lacks
// `version`), the operator never writes its spec, and custom-resource
// validation runs against the whole object on every write including
// status-subresource patches. If marking `version` required invalidated
// status patches against that stored object, the singleton would hot-loop on
// patch errors with no self-heal path.
//
// This test stores an object under the old schema, swaps in the reshaped
// schema (required `version` inside a map value), and patches status the way
// the reconciler does. The API server's validation ratcheting (unchanged
// correlatable fields are not re-validated; the spec is definitionally
// unchanged on a status patch) is expected to let the patch through — the
// measured outcome that selected the `+required` marker on
// api/v1alpha1.Subscription.Version. If a Kubernetes bump flips this test,
// the marker decision must be revisited before shipping.
func TestStatusPatchSurvivesRequiredFieldRatchet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	testEnv := &envtest.Environment{}
	if dir := firstEnvtestBinaryDir(t); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testEnv.Stop())
	})

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	// Install the old schema (subscription = {enable, filter}) and wait for it
	// to serve.
	crd := ratchetCRD(oldSubscriptionSchema())
	require.NoError(t, k8sClient.Create(ctx, crd))
	requireEventually(t, func() bool {
		var cur apiextensionsv1.CustomResourceDefinition
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crd), &cur); err != nil {
			return false
		}
		for _, cond := range cur.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true
			}
		}
		return false
	}, "CRD should become Established")

	// Store the legacy object: a filter, no version — the shape of the
	// pre-reshape `cluster` singleton.
	legacy := ratchetObject("cluster")
	require.NoError(t, unstructured.SetNestedMap(legacy.Object,
		map[string]any{"filter": map[string]any{"range": ">=1.0.0-alpha"}},
		"spec", "registry", "opmodel.dev/catalogs/opm"))
	requireEventually(t, func() bool {
		return k8sClient.Create(ctx, legacy.DeepCopy()) == nil
	}, "legacy object should be storable under the old schema")

	// Swap in the reshaped schema: subscription = {enable, version} with
	// version required + minLength=1.
	var cur apiextensionsv1.CustomResourceDefinition
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(crd), &cur))
	cur.Spec.Versions[0].Schema = &apiextensionsv1.CustomResourceValidation{
		OpenAPIV3Schema: newSubscriptionSchema(),
	}
	require.NoError(t, k8sClient.Update(ctx, &cur))

	// The structural schema propagates asynchronously; a versionless CREATE is
	// always fully validated, so its rejection proves the new schema is
	// enforced before the measurement runs.
	requireEventually(t, func() bool {
		probe := ratchetObject("probe")
		require.NoError(t, unstructured.SetNestedMap(probe.Object,
			map[string]any{"enable": true},
			"spec", "registry", "opmodel.dev/catalogs/opm"))
		err := k8sClient.Create(ctx, probe)
		if err == nil {
			_ = k8sClient.Delete(ctx, probe)
			return false
		}
		return apierrors.IsInvalid(err)
	}, "versionless create should be rejected once the new schema serves")

	// The measurement: patch status on the stored legacy object exactly the
	// way the reconciler does (status subresource, spec untouched).
	stored := ratchetObject("cluster")
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(stored), stored))
	base := stored.DeepCopy()
	require.NoError(t, unstructured.SetNestedField(stored.Object, int64(7),
		"status", "observedGeneration"))
	patchErr := k8sClient.Status().Patch(ctx, stored, client.MergeFrom(base))

	assert.NoError(t, patchErr,
		"validation ratcheting must let a status patch through against a stored object missing "+
			"the now-required version; if this fails, Subscription.Version cannot ship CRD-required")
}

// ratchetCRD builds a cluster-scoped CRD with a status subresource whose spec
// mirrors the Platform registry shape: a map of subscription objects. The
// group is test-only so the measurement never collides with the real Platform
// CRD.
func ratchetCRD(subscription *apiextensionsv1.JSONSchemaProps) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "ratchetplatforms.ratchet.test.opmodel.dev"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "ratchet.test.opmodel.dev",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "ratchetplatforms",
				Singular: "ratchetplatform",
				Kind:     "RatchetPlatform",
				ListKind: "RatchetPlatformList",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: rootSchema(subscription),
				},
			}},
		},
	}
}

func rootSchema(subscription *apiextensionsv1.JSONSchemaProps) *apiextensionsv1.JSONSchemaProps {
	return &apiextensionsv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"spec": {
				Type:     "object",
				Required: []string{"type"},
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"type": {Type: "string", MinLength: new(int64(1))},
					"registry": {
						Type: "object",
						AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{
							Schema: subscription,
						},
					},
				},
			},
			"status": {
				Type: "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"observedGeneration": {Type: "integer", Format: "int64"},
				},
			},
		},
	}
}

// oldSubscriptionSchema mirrors the pre-reshape Subscription: optional enable,
// optional filter{range,allow,deny}.
func oldSubscriptionSchema() *apiextensionsv1.JSONSchemaProps {
	return &apiextensionsv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"enable": {Type: "boolean"},
			"filter": {
				Type: "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"range": {Type: "string"},
					"allow": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
						Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"},
					}},
					"deny": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
						Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"},
					}},
				},
			},
		},
	}
}

// newSubscriptionSchema mirrors the reshaped Subscription: optional enable,
// required version with minLength=1 (the `+required` posture under
// measurement).
func newSubscriptionSchema() *apiextensionsv1.JSONSchemaProps {
	return &apiextensionsv1.JSONSchemaProps{
		Type:     "object",
		Required: []string{"version"},
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"enable":  {Type: "boolean"},
			"version": {Type: "string", MinLength: new(int64(1))},
		},
	}
}

func ratchetObject(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(runtimeschema.GroupVersionKind{
		Group: "ratchet.test.opmodel.dev", Version: "v1alpha1", Kind: "RatchetPlatform",
	})
	u.SetName(name)
	_ = unstructured.SetNestedField(u.Object, "kubernetes", "spec", "type")
	return u
}

func requireEventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 30*time.Second, 250*time.Millisecond, msg)
}

func firstEnvtestBinaryDir(t *testing.T) string {
	t.Helper()
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
