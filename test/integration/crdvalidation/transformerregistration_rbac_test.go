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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// Creating a TransformerRegistration is a platform-admin privilege
// (enhancement 0015 D3). A transformer is arbitrary CUE producing arbitrary
// Kubernetes objects, so the ability to register one is the ability to change
// how another team's traits render.
//
// The gate works by absence: rendered objects reach the cluster through a
// ServiceAccount the operator impersonates per tenant, and this repo ships no
// tenant-facing role granting create on the kind. The test drives that
// impersonation path directly, so what it measures is what the operator's
// apply would hit.
//
// This lives beside the admission tests because it answers the other half of
// the same question — what the API server does with a claim someone submits —
// with authorization rather than schema doing the refusing.
//
// The ClusterRole is read from config/rbac/ rather than written inline, so the
// test asserts the artifact the operator actually ships. An edit that widens
// or narrows the shipped role moves this test with it.
func TestTransformerRegistrationRBAC(t *testing.T) {
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
	admin, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	const (
		tenantNamespace = "default"
		tenantName      = "tenant"
		tenantSubject   = "system:serviceaccount:" + tenantNamespace + ":" + tenantName
	)

	require.NoError(t, admin.Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: tenantName, Namespace: tenantNamespace},
	}))

	// The role as shipped, not a copy of it.
	roleYAML, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "config", "rbac", "transformerregistration_admin_role.yaml"))
	require.NoError(t, err)
	adminRole := &rbacv1.ClusterRole{}
	require.NoError(t, yaml.Unmarshal(roleYAML, adminRole))
	require.NotEmpty(t, adminRole.Name, "the shipped role must parse")

	// Impersonate the tenant ServiceAccount exactly as internal/apply does.
	tenantCfg := rest.CopyConfig(cfg)
	tenantCfg.Impersonate = rest.ImpersonationConfig{UserName: tenantSubject}
	tenant, err := client.New(tenantCfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	claim := func(name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "opmodel.dev/v1alpha1",
			"kind":       "TransformerRegistration",
			"metadata":   map[string]any{"name": name},
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

	t.Run("an unbound tenant ServiceAccount is refused the create", func(t *testing.T) {
		err := tenant.Create(ctx, claim("backup-system.unbound"))
		require.Error(t, err)
		require.True(t, apierrors.IsForbidden(err),
			"the refusal must be authorization, not validation: got %v", err)
		require.Contains(t, err.Error(), tenantSubject,
			"the refusal names the subject, which is how it surfaces as an impersonation failure")
	})

	t.Run("the shipped role is not bound by default", func(t *testing.T) {
		var bindings rbacv1.ClusterRoleBindingList
		require.NoError(t, admin.List(ctx, &bindings))
		for _, b := range bindings.Items {
			require.NotEqual(t, adminRole.Name, b.RoleRef.Name,
				"the platform-admin role ships unbound; binding it is the cluster administrator's decision")
		}
	})

	t.Run("a subject bound to the shipped role may create a claim", func(t *testing.T) {
		require.NoError(t, admin.Create(ctx, adminRole))
		require.NoError(t, admin.Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-as-platform-admin"},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     adminRole.Name,
			},
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      tenantName,
				Namespace: tenantNamespace,
			}},
		}))

		// RBAC reaches the authorizer's cache asynchronously.
		bound := claim("backup-system.bound")
		requireEventually(t, func() bool { return tenant.Create(ctx, bound) == nil },
			"the binding must take effect and the create succeed")
		t.Cleanup(func() { require.NoError(t, admin.Delete(ctx, bound)) })
	})
}
