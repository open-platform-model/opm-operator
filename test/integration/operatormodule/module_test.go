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

package operatormodule_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fixedSelector is the controller Deployment's selector as the first module
// version renders it. Kubernetes never lets an apply change a selector, so no
// module version, catalog or core pin, or value may change it: a difference
// here means every upgrade would fail on an immutable field.
var fixedSelector = map[string]any{
	"app.kubernetes.io/name":           "controller-manager",
	"component.opmodel.dev/name":       "controller-manager",
	"control-plane":                    "controller-manager",
	"core.opmodel.dev/workload-type":   "stateless",
	"module-instance.opmodel.dev/name": "opm-operator",
}

// adminRoleNames are the administrator ClusterRoles the operator ships
// unbound, the only objects the module renders as raw objects.
var adminRoleNames = []string{
	"opm-operator-metrics-reader",
	"opm-operator-moduleinstance-admin-role",
	"opm-operator-moduleinstance-editor-role",
	"opm-operator-moduleinstance-viewer-role",
	"opm-operator-platform-viewer-role",
	"opm-operator-modulepackage-viewer-role",
	"opm-operator-transformerregistration-viewer-role",
	"opm-operator-transformerregistration-admin-role",
}

const (
	// objectsTransformer is the catalog's raw-objects transformer.
	objectsTransformer = "objects-transformer"

	kindRole        = "Role"
	kindClusterRole = "ClusterRole"
)

var _ = Describe("The operator module", func() {
	// Needs no registry, so it runs before any render and is enforced
	// without GHCR too.
	Context("the operator release it names", func() {
		It("is at or above the minimum operator version", func() {
			Expect(checkMinOperatorVersion(moduleDir, minVersionFile)).To(Succeed())
		})

		It("refuses an older operator, naming both versions", func() {
			dir := scratchModule()
			path := filepath.Join(dir, "operator", "operator.cue")
			b, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			op, err := readModuleOperator(moduleDir)
			Expect(err).NotTo(HaveOccurred())
			older := strings.Replace(string(b), fmt.Sprintf("Version: %q", op.Version), `Version: "1.0.0-beta.5"`, 1)
			Expect(older).NotTo(Equal(string(b)), "the scratch copy names another version")
			Expect(os.WriteFile(path, []byte(older), 0o644)).To(Succeed())

			minTag, err := os.ReadFile(minVersionFile)
			Expect(err).NotTo(HaveOccurred())
			err = checkMinOperatorVersion(dir, minVersionFile)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("v1.0.0-beta.5"))
			Expect(err.Error()).To(ContainSubstring(strings.TrimSpace(string(minTag))))
		})
	})

	Context("rendered with default values", Ordered, func() {
		var objs []obj

		BeforeAll(func() {
			objs = mustRender(moduleDir)
		})

		It("renders the 22 objects of the install with their fixed names", func() {
			type key struct{ kind, namespace, name string }
			got := make([]key, 0, len(objs))
			for _, o := range objs {
				got = append(got, key{o.kind(), o.namespace(), o.name()})
			}
			ns := instanceNamespace
			Expect(got).To(ConsistOf(
				key{"CustomResourceDefinition", "", "moduleinstances.opmodel.dev"},
				key{"CustomResourceDefinition", "", "modulepackages.opmodel.dev"},
				key{"CustomResourceDefinition", "", "platforms.opmodel.dev"},
				key{"CustomResourceDefinition", "", "transformerregistrations.opmodel.dev"},
				key{"Namespace", "", "opm-operator-system"},
				key{"ServiceAccount", ns, "opm-operator-controller-manager"},
				key{"Deployment", ns, "opm-operator-controller-manager"},
				key{"Service", ns, "opm-operator-controller-manager-metrics-service"},
				key{"Role", ns, "opm-operator-leader-election-role"},
				key{"RoleBinding", ns, "opm-operator-leader-election-role"},
				key{"ClusterRole", "", "opm-operator-manager-role"},
				key{"ClusterRoleBinding", "", "opm-operator-manager-role"},
				key{"ClusterRole", "", "opm-operator-metrics-auth-role"},
				key{"ClusterRoleBinding", "", "opm-operator-metrics-auth-role"},
				key{"ClusterRole", "", "opm-operator-metrics-reader"},
				key{"ClusterRole", "", "opm-operator-moduleinstance-admin-role"},
				key{"ClusterRole", "", "opm-operator-moduleinstance-editor-role"},
				key{"ClusterRole", "", "opm-operator-moduleinstance-viewer-role"},
				key{"ClusterRole", "", "opm-operator-platform-viewer-role"},
				key{"ClusterRole", "", "opm-operator-modulepackage-viewer-role"},
				key{"ClusterRole", "", "opm-operator-transformerregistration-viewer-role"},
				key{"ClusterRole", "", "opm-operator-transformerregistration-admin-role"},
			))
		})

		It("renders the fixed Deployment selector, with control-plane on the pods", func() {
			d := find(objs, "Deployment", "opm-operator-controller-manager")
			Expect(dict(d.Object, "spec", "selector", "matchLabels")).To(Equal(fixedSelector),
				"the selector differs from the fixed one; a module with another selector fails every upgrade")
			Expect(dict(d.Object, "spec", "template", "metadata", "labels")).To(
				HaveKeyWithValue("control-plane", "controller-manager"))
		})

		It("binds each controller role to the controller's ServiceAccount under the catalog's binding name", func() {
			for _, b := range []struct{ kind, name, roleKind string }{
				{"ClusterRoleBinding", "opm-operator-manager-role", "ClusterRole"},
				{"ClusterRoleBinding", "opm-operator-metrics-auth-role", "ClusterRole"},
				{"RoleBinding", "opm-operator-leader-election-role", "Role"},
			} {
				o := find(objs, b.kind, b.name)
				Expect(dict(o.Object, "roleRef")).To(HaveKeyWithValue("name", b.name))
				Expect(dict(o.Object, "roleRef")).To(HaveKeyWithValue("kind", b.roleKind))
				Expect(list(o.Object, "subjects")).To(ConsistOf(map[string]any{
					"kind": "ServiceAccount", "name": "opm-operator-controller-manager", "namespace": instanceNamespace,
				}))
			}
		})

		It("renders raw objects only for the eight administrator ClusterRoles, in one component", func() {
			var rawComponents []string
			var rawObjects []string
			for _, o := range objs {
				if !strings.Contains(o.Transformer, objectsTransformer) {
					continue
				}
				rawComponents = append(rawComponents, o.Component)
				Expect(o.kind()).To(Equal(kindClusterRole), "raw object %s", o.name())
				rawObjects = append(rawObjects, o.name())
			}
			Expect(sortedUnique(rawComponents)).To(Equal([]string{"admin-roles"}))
			Expect(rawObjects).To(ConsistOf(adminRoleNames))
		})

		It("binds none of the administrator ClusterRoles", func() {
			for _, o := range objs {
				if o.kind() != "ClusterRoleBinding" && o.kind() != "RoleBinding" {
					continue
				}
				Expect(adminRoleNames).NotTo(ContainElement(str(o.Object, "roleRef", "name")),
					"%s %s binds an administrator role", o.kind(), o.name())
			}
		})

		It("renders each CRD's spec and controller-gen annotation equal to config/crd/bases", func() {
			want := configCRDs(filepath.Join(repoRoot, "config"))
			Expect(want).To(HaveLen(4))
			for name, crd := range want {
				got := find(objs, "CustomResourceDefinition", name)
				Expect(normalize(got.Object["spec"])).To(Equal(normalize(crd["spec"])), "CRD %s spec", name)
				const genVersion = "controller-gen.kubebuilder.io/version"
				Expect(dict(got.Object, "metadata", "annotations")).To(HaveKeyWithValue(
					genVersion, str(crd, "metadata", "annotations", genVersion)))
			}
		})

		It("renders every role's rules equal to config/rbac, and exactly the generated role set", func() {
			want := configRoles(filepath.Join(repoRoot, "config"))
			var rendered []string
			for _, o := range objs {
				if o.kind() == kindRole || o.kind() == kindClusterRole {
					rendered = append(rendered, strings.TrimPrefix(o.name(), instanceName+"-"))
				}
			}
			sort.Strings(rendered)
			Expect(rendered).To(Equal(rbacSourceNames(moduleDir)), "the rendered roles are the module's generated RBAC data")
			Expect(rendered).To(ConsistOf(keys(want)))
			for name, role := range want {
				got := find(objs, str(role, "kind"), instanceName+"-"+name)
				Expect(normalize(got.Object["rules"])).To(Equal(normalize(role["rules"])), "rules of %s", name)
			}
		})

		It("runs the operator release the module names, pulled IfNotPresent", func() {
			op, err := readModuleOperator(moduleDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(op.Tag).To(Equal("v" + op.Version))
			Expect(op.Digest).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
			c := managerContainer(objs)
			Expect(c["image"]).To(Equal(fmt.Sprintf("%s:v%s@%s", op.Repository, op.Version, op.Digest)))
			Expect(c["imagePullPolicy"]).To(Equal("IfNotPresent"))
		})
	})
})

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
