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
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs change a scratch copy of the module or of config/ and render
// the copy, to prove what the module does with controller output it has not
// seen yet.
var _ = Describe("The operator module on changed controller output", func() {
	It("refuses a CRD field the catalog cannot carry, naming it", func() {
		dir := scratchModule()
		// The same as a CRD whose imported spec gained spec.conversion.
		Expect(os.WriteFile(filepath.Join(dir, "zz_scratch_conversion.cue"), []byte(
			"package opm_operator\n\n#crdSource: \"platforms.opmodel.dev\": spec: conversion: strategy: \"None\"\n",
		), 0o644)).To(Succeed())

		_, err := render(dir, instanceName, instanceNamespace, "{}")
		Expect(err).To(HaveOccurred(), "the render must refuse, not drop the field")
		Expect(err.Error()).To(ContainSubstring("conversion"))
	})

	It("refuses CRD metadata the catalog cannot carry, naming it", func() {
		dir := scratchModule()
		// The same as a CRD manifest that gained metadata.labels.
		Expect(os.WriteFile(filepath.Join(dir, "zz_scratch_labels.cue"), []byte(
			"package opm_operator\n\n#crdSource: \"platforms.opmodel.dev\": metadata: labels: team: \"opm\"\n",
		), 0o644)).To(Succeed())

		_, err := render(dir, instanceName, instanceNamespace, "{}")
		Expect(err).To(HaveOccurred(), "the render must refuse, not drop the labels")
		Expect(errText(err)).To(ContainSubstring("platforms.opmodel.dev carries metadata.labels"))
	})

	It("carries a verb added to a controller RBAC marker into the manager ClusterRole, and nothing else", func() {
		requireTool("cue")
		before := mustRender(moduleDir)

		// The marker change as `task dev:manifests` would write it.
		config := filepath.Join(GinkgoT().TempDir(), "config")
		copyTree(filepath.Join(repoRoot, "config"), config)
		rolePath := filepath.Join(config, "rbac", "role.yaml")
		b, err := os.ReadFile(rolePath)
		Expect(err).NotTo(HaveOccurred())
		const anchor = "  - serviceaccounts\n  verbs:\n  - get\n"
		Expect(string(b)).To(ContainSubstring(anchor), "config/rbac/role.yaml grants get on serviceaccounts")
		withList := strings.Replace(string(b), anchor, anchor+"  - list\n", 1)
		Expect(os.WriteFile(rolePath, []byte(withList), 0o644)).To(Succeed())

		dir := scratchModule()
		runScript(repoRoot, []string{"SRC=" + mustAbs(config), "OUT=" + dir}, "hack/operator-module/generate.sh")
		after := mustRender(dir)

		// The manager ClusterRole gained exactly the verb.
		oldRules := list(find(before, "ClusterRole", "opm-operator-manager-role").Object, "rules")
		newRules := list(find(after, "ClusterRole", "opm-operator-manager-role").Object, "rules")
		Expect(newRules).To(HaveLen(len(oldRules)))
		changed := 0
		for i := range oldRules {
			o, n := oldRules[i].(map[string]any), newRules[i].(map[string]any)
			coreGroup := slices.Equal(toStrings(o["apiGroups"]), []string{""})
			if coreGroup && slices.Contains(toStrings(o["resources"]), "serviceaccounts") {
				Expect(toStrings(n["verbs"])).To(ConsistOf(append(toStrings(o["verbs"]), "list")))
				delete(o, "verbs")
				delete(n, "verbs")
				changed++
			}
			Expect(n).To(Equal(o), "rule %d is otherwise unchanged", i)
		}
		Expect(changed).To(Equal(1))

		// Every other object renders the same.
		for _, o := range before {
			if o.kind() == "ClusterRole" && o.name() == "opm-operator-manager-role" {
				continue
			}
			// The instance and module uuids derive from the module path and
			// the instance coordinates, which the scratch copy keeps.
			Expect(find(after, o.kind(), o.name()).Object).To(Equal(o.Object), "%s %s is unchanged", o.kind(), o.name())
		}
	})
})

func mustAbs(p string) string {
	GinkgoHelper()
	abs, err := filepath.Abs(p)
	Expect(err).NotTo(HaveOccurred())
	return abs
}

func toStrings(v any) []string {
	l := v.([]any)
	out := make([]string, 0, len(l))
	for _, e := range l {
		out = append(out, e.(string))
	}
	return out
}
