// Package rbac tests the roles the operator ships for users.
package rbac

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// statusWriters returns the rules of role that let its holder write a status
// subresource: a write verb on a resource that is, or by wildcard covers,
// "<resource>/status".
func statusWriters(role rbacv1.ClusterRole) []rbacv1.PolicyRule {
	var out []rbacv1.PolicyRule
	for _, rule := range role.Rules {
		if coversStatus(rule.Resources) && writes(rule.Verbs) {
			out = append(out, rule)
		}
	}
	return out
}

func coversStatus(resources []string) bool {
	for _, r := range resources {
		if r == "*" || r == "*/*" || strings.HasSuffix(r, "/status") {
			return true
		}
	}
	return false
}

func writes(verbs []string) bool {
	for _, v := range verbs {
		if v == "update" || v == "patch" || v == "*" {
			return true
		}
	}
	return false
}

// userRoleFiles are the roles under config/rbac meant for users: the editor,
// admin and viewer roles of every kind.
func userRoleFiles(t *testing.T) []string {
	t.Helper()
	files := make([]string, 0, 16)
	for _, suffix := range []string{"_editor_role.yaml", "_admin_role.yaml", "_viewer_role.yaml"} {
		matches, err := filepath.Glob(filepath.Join("..", "..", "config", "rbac", "*"+suffix))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no user role found under config/rbac")
	}
	return files
}

// The Ready reason of a deleting ModuleInstance or ModulePackage is the
// record that its cleanup sent every delete (internal/reconcile,
// everyDeleteWasSent). A user who could write the status subresource could
// forge it, so no role the operator ships for users may grant that.
func TestUserRolesCannotWriteAStatusSubresource(t *testing.T) {
	sawStatusRule := false
	for _, file := range userRoleFiles(t) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var role rbacv1.ClusterRole
		if err := yaml.UnmarshalStrict(data, &role); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if len(role.Rules) == 0 {
			t.Errorf("%s: no rules read; the test would pass on anything", file)
		}
		for _, rule := range role.Rules {
			sawStatusRule = sawStatusRule || coversStatus(rule.Resources)
		}
		for _, rule := range statusWriters(role) {
			t.Errorf("%s grants %v on %v: a user must not write a status subresource", file, rule.Verbs, rule.Resources)
		}
	}
	if !sawStatusRule {
		t.Error("no shipped user role names a status subresource; check that the files are still read")
	}
}

// The check fails when a write verb is added to a copy of a shipped role.
func TestStatusWritersFindsAnAddedVerb(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "rbac", "moduleinstance_editor_role.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"update", "patch", "*"} {
		var role rbacv1.ClusterRole
		if err := yaml.UnmarshalStrict(data, &role); err != nil {
			t.Fatal(err)
		}
		changed := false
		for i, rule := range role.Rules {
			if coversStatus(rule.Resources) {
				role.Rules[i].Verbs = append(role.Rules[i].Verbs, verb)
				changed = true
			}
		}
		if !changed {
			t.Fatal("the editor role names no status subresource")
		}
		if len(statusWriters(role)) == 0 {
			t.Errorf("verb %q on the status subresource was not found", verb)
		}
	}
	wildcard := rbacv1.ClusterRole{Rules: []rbacv1.PolicyRule{{Resources: []string{"*"}, Verbs: []string{"patch"}}}}
	if len(statusWriters(wildcard)) == 0 {
		t.Error("a write verb on every resource was not found")
	}
}
