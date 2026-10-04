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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"
)

// viewerRole is one shipped viewer ClusterRole and the resource it reads.
type viewerRole struct {
	file     string
	resource string
}

var viewerRoles = []viewerRole{
	{file: "platform_viewer_role.yaml", resource: "platforms"},
	{file: "modulepackage_viewer_role.yaml", resource: "modulepackages"},
	{file: "transformerregistration_viewer_role.yaml", resource: "transformerregistrations"},
}

func loadShippedRole(t *testing.T, file string) *rbacv1.ClusterRole {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "rbac", file))
	require.NoError(t, err)
	role := &rbacv1.ClusterRole{}
	require.NoError(t, yaml.Unmarshal(raw, role))
	require.NotEmpty(t, role.Name, "the shipped role %s must parse", file)
	return role
}

// The viewer roles let a cluster administrator grant a non-admin read access
// to Platforms, ModulePackages and TransformerRegistrations (0030:D11). They
// ship unbound and outside the built-in view role; whether they aggregate
// into it is still an open question, and adding the label later is the
// reversible direction.
//
// The roles are read from config/rbac/ rather than written inline, so the test
// asserts the artifacts the operator actually ships.
func TestViewerRolesShape(t *testing.T) {
	for _, vr := range viewerRoles {
		t.Run(vr.file, func(t *testing.T) {
			role := loadShippedRole(t, vr.file)

			for label := range role.Labels {
				require.False(t, strings.HasPrefix(label, "rbac.authorization.k8s.io/aggregate-to-"),
					"%s must not aggregate into a built-in role: found label %s", role.Name, label)
			}
			require.Nil(t, role.AggregationRule, "%s must not aggregate other roles", role.Name)

			want := []rbacv1.PolicyRule{
				{
					APIGroups: []string{"opmodel.dev"},
					Resources: []string{vr.resource},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"opmodel.dev"},
					Resources: []string{vr.resource + "/status"},
					Verbs:     []string{"get"},
				},
			}
			require.Equal(t, want, role.Rules, "%s grants reads on its own kind only", role.Name)
		})
	}
}

// The manifest ships no binding naming a viewer role. A cluster started for a
// test never applies the manifest, so this reads every binding under config/,
// the tree the install manifest and the operator module are rendered from.
func TestViewerRolesShipUnbound(t *testing.T) {
	names := map[string]bool{}
	for _, vr := range viewerRoles {
		names[loadShippedRole(t, vr.file).Name] = true
	}
	root := filepath.Join("..", "..", "..", "config")
	bindings := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for doc := range strings.SplitSeq(string(raw), "\n---") {
			var b struct {
				Kind    string         `json:"kind"`
				RoleRef rbacv1.RoleRef `json:"roleRef"`
			}
			if yaml.Unmarshal([]byte(doc), &b) != nil {
				continue
			}
			if b.Kind != "RoleBinding" && b.Kind != "ClusterRoleBinding" {
				continue
			}
			bindings++
			require.False(t, names[b.RoleRef.Name], "%s binds %s, which ships unbound", path, b.RoleRef.Name)
		}
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, bindings, "the scan must see the operator's own bindings")
}

func TestViewerRolesRBAC(t *testing.T) {
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
	admin, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	const (
		viewer        = "portal-viewer"
		boundNS       = "shop"
		otherNS       = "billing"
		platformRole  = "platform_viewer_role.yaml"
		packageRole   = "modulepackage_viewer_role.yaml"
		claimRoleFile = "transformerregistration_viewer_role.yaml"
	)

	// allowed asks the API server's authorizer, through a SubjectAccessReview,
	// whether the viewer may perform verb on resource (and subresource) in ns.
	allowed := func(verb, resource, subresource, ns string) bool {
		t.Helper()
		sar := &authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				User: viewer,
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   ns,
					Verb:        verb,
					Group:       "opmodel.dev",
					Resource:    resource,
					Subresource: subresource,
				},
			},
		}
		require.NoError(t, admin.Create(ctx, sar))
		return sar.Status.Allowed
	}
	// scope gives the namespace a request on resource is checked in: the
	// bound namespace for ModulePackages, none for the cluster-scoped kinds.
	scope := func(resource string) string {
		if resource == "modulepackages" {
			return boundNS
		}
		return ""
	}

	t.Run("an unbound user reads nothing", func(t *testing.T) {
		for _, vr := range viewerRoles {
			for _, verb := range []string{"get", "list"} {
				require.False(t, allowed(verb, vr.resource, "", scope(vr.resource)),
					"%s %s must be refused before any binding", verb, vr.resource)
			}
		}
	})

	// Bind as the install page documents: the cluster-scoped kinds through a
	// ClusterRoleBinding, ModulePackages through a RoleBinding in one namespace.
	for _, vr := range viewerRoles {
		require.NoError(t, admin.Create(ctx, loadShippedRole(t, vr.file)))
	}
	subject := []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: viewer}}
	for _, file := range []string{platformRole, claimRoleFile} {
		name := loadShippedRole(t, file).Name
		require.NoError(t, admin.Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: viewer + "-" + name},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name},
			Subjects:   subject,
		}))
	}
	require.NoError(t, admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: boundNS}}))
	require.NoError(t, admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: viewer + "-packages", Namespace: boundNS},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     loadShippedRole(t, packageRole).Name,
		},
		Subjects: subject,
	}))

	t.Run("a bound user reads every kind and its status", func(t *testing.T) {
		for _, vr := range viewerRoles {
			ns := scope(vr.resource)
			// RBAC reaches the authorizer's cache asynchronously.
			requireEventually(t, func() bool { return allowed("get", vr.resource, "", ns) },
				"the binding must take effect for "+vr.resource)
			for _, verb := range []string{"list", "watch"} {
				require.True(t, allowed(verb, vr.resource, "", ns), "%s %s", verb, vr.resource)
			}
			require.True(t, allowed("get", vr.resource, "status", ns), "get %s/status", vr.resource)
		}
	})

	t.Run("a namespace binding does not reach other namespaces", func(t *testing.T) {
		require.False(t, allowed("list", "modulepackages", "", otherNS))
		require.False(t, allowed("list", "modulepackages", "", ""),
			"a RoleBinding must not grant a cluster-wide list")
	})

	t.Run("a viewer cannot change what it reads", func(t *testing.T) {
		writes := []string{"create", "update", "patch", "delete", "deletecollection"}
		for _, vr := range viewerRoles {
			ns := scope(vr.resource)
			for _, verb := range writes {
				require.False(t, allowed(verb, vr.resource, "", ns), "%s %s", verb, vr.resource)
			}
			for _, verb := range []string{"update", "patch"} {
				require.False(t, allowed(verb, vr.resource, "status", ns), "%s %s/status", verb, vr.resource)
			}
		}
		require.False(t, allowed("get", "moduleinstances", "", boundNS),
			"the viewer roles grant no ModuleInstance read")
	})
}
